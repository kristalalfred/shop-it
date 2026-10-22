import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {createContext, runInContext} from 'node:vm';

const source = await readFile(new URL('../../internal/backend/plugin/app.js', import.meta.url), 'utf8');
class Element {
  constructor(tag = 'div') { this.tagName = tag; this.children = []; this.listeners = {}; this.attributes = {}; this.dataset = {}; this.text = ''; this.value = ''; }
  set textContent(value) { this.text = String(value); this.children = []; }
  get textContent() { return this.text + this.children.map((n) => n.textContent).join(' '); }
  append(...children) { this.children.push(...children); }
  replaceChildren(...children) { this.text = ''; this.children = children; }
  setAttribute(name, value) { this.attributes[name] = value; }
  addEventListener(name, fn) { this.listeners[name] = fn; }
  matches(selector) { return selector.split(',').some((s) => s === this.tagName || s === 'details.grocery' && this.tagName === 'details' && this.className?.includes('grocery')); }
  querySelectorAll(selector) { return this.children.flatMap((n) => [...(n.matches(selector) ? [n] : []), ...n.querySelectorAll(selector)]); }
  focus() {}
  select() {}
  click() { if (!this.disabled) return this.listeners.click?.({preventDefault() {}}); }
}
const settle = async () => { for (let i = 0; i < 6; i++) await new Promise((r) => setImmediate(r)); };
function initial() {
  return {preferences: {draft: '', retailer_ids: []}, browser_online: false, setup_url: 'http://127.0.0.1:18081/plugin', demo: false, stores: [{id: 'willys', name: 'Willys', mode: 'browser', url: 'https://www.willys.se/varukorg', selected: false}]};
}
async function app({embedded = false, snapshot = initial()} = {}) {
  const root = new Element(), token = new Element('input'), listeners = new Map(), messages = [], requests = [], intervals = [];
  let state = structuredClone(snapshot), storeStatus = 'setup_required';
  const service = (name, args) => {
    requests.push({name, args: structuredClone(args)});
    if (name === 'configure_ship_it') {
      if (args.draft !== undefined) state.preferences.draft = args.draft;
      if (args.retailer_ids) { state.preferences.retailer_ids = args.retailer_ids; state.stores.forEach((s) => { s.selected = args.retailer_ids.includes(s.id); }); }
    }
    if (name === 'connect_store') state.stores[0].session = {status: storeStatus, label: 'Willys Stockholm'};
    return structuredClone(state);
  };
  const parent = {postMessage(message) {
    messages.push(structuredClone(message));
    if (message.id != null) queueMicrotask(() => listeners.get('message')?.({source: parent, data: {jsonrpc: '2.0', id: message.id, result: message.method === 'tools/call' ? {structuredContent: service(message.params.name, message.params.arguments)} : message.method === 'ui/initialize' ? {hostContext: {theme: 'light'}} : {}}}));
  }};
  const window = {SHIP_IT: embedded ? {} : {token: 'local-user'}, parent, addEventListener: (name, fn) => listeners.set(name, fn)};
  const context = createContext({window, document: {getElementById: (id) => id === 'app' ? root : token, createElement: (tag) => new Element(tag), documentElement: {dataset: {}, scrollHeight: 600}, activeElement: null, addEventListener() {}, dispatchEvent() {}},
    CustomEvent: class {constructor(type) {this.type = type;}}, crypto: {randomUUID: () => 'stable-request'},
    setInterval: (fn) => { intervals.push(fn); return intervals.length; }, clearInterval() {}, setTimeout: () => 1, clearTimeout() {},
    fetch: async (url, options) => ({ok: true, json: async () => service(url.split('/').at(-1), JSON.parse(options.body))}),
  });
  runInContext(source, context); await settle();
  return {root, requests, messages, listeners, parent, intervals,
    setOnline: (value) => { state.browser_online = value; },
    setStatus: (value) => { storeStatus = value; },
    setState: (value) => {state = structuredClone(value);},
    find: (tag, text) => root.querySelectorAll(tag).find((n) => n.textContent === text || n.attributes['aria-label'] === text),
  };
}

test('onboarding saves the request, retains store choices, and resumes a store that still needs setup', async () => {
  const ui = await app();
  const input = ui.find('textarea', 'Your shopping request'); input.value = 'Five dinners for two, under 800 kr'; input.listeners.input();
  await ui.find('button', 'Continue ↗').click(); await settle();
  assert.equal(ui.requests.at(-1).args.draft, 'Five dinners for two, under 800 kr');
  await ui.find('button', 'Willys').click(); await settle();
  await ui.find('button', 'Connect stores ↗').click(); await settle();
  assert.match(ui.root.textContent, /Connect your browser/);
  assert.equal(ui.find('button', 'Connect').disabled, true);
  ui.setOnline(true); await ui.intervals[0](); await settle();
  await ui.find('button', 'Connect').click(); await settle();
  assert.match(ui.root.textContent, /Choose your store or delivery/);
  assert.equal(ui.find('button', 'Continue with ChatGPT ↗').disabled, true);
  ui.setStatus('ready'); await ui.find('button', 'Check again').click(); await settle();
  assert.match(ui.root.textContent, /You’re ready to shop/);
  assert.equal(ui.find('button', 'Continue with ChatGPT ↗').disabled, false);
  assert.equal(ui.requests.find((r) => r.name === 'configure_ship_it').args.draft, 'Five dinners for two, under 800 kr');
});

test('the embedded app initializes the MCP Apps bridge and hands the saved request to the conversation', async () => {
  const state = initial(); state.preferences = {draft: 'Vegetarian dinners for two', retailer_ids: ['willys']}; state.browser_online = true; state.stores[0].selected = true; state.stores[0].session = {status: 'ready'};
  const ui = await app({embedded: true, snapshot: state});
  assert.equal(ui.messages[0].method, 'ui/initialize');
  assert.equal(ui.messages[0].params.protocolVersion, '2026-01-26');
  assert.ok(ui.messages.some((m) => m.method === 'ui/notifications/initialized'));
  await ui.find('button', 'Find my groceries ↗').click(); await settle();
  const message = ui.messages.find((m) => m.method === 'ui/message');
  assert.equal(message.params.role, 'user');
  assert.match(message.params.content[0].text, /Vegetarian dinners for two/);
  assert.equal(ui.requests.some((r) => r.name === 'create_shopping_plan'), false);
  ui.listeners.get('message')({source: {}, data: {jsonrpc: '2.0', method: 'ui/notifications/tool-result', params: {structuredContent: {...state, stores: [{name: 'Injected state'}]}}}});
  assert.equal(ui.root.textContent.includes('Injected state'), false);
});
