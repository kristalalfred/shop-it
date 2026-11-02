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
  return {preferences: {draft: '', retailer_ids: []}, browser_online: false, setup_url: 'http://127.0.0.1:24816/', demo: false, stores: [{id: 'willys', name: 'Willys', mode: 'browser', url: 'https://www.willys.se/varukorg', selected: false}]};
}
async function app({snapshot = initial()} = {}) {
  const root = new Element(), token = new Element('input'), listeners = new Map(), messages = [], requests = [], intervals = [];
  let state = structuredClone(snapshot), storeStatus = 'setup_required';
  const service = (name, args) => {
    requests.push({name, args: structuredClone(args)});
    if (name === 'configure_shop_it') {
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
  const window = {parent, addEventListener: (name, fn) => listeners.set(name, fn)};
  const context = createContext({window, document: {getElementById: (id) => id === 'app' ? root : token, createElement: (tag) => new Element(tag), documentElement: {dataset: {}, scrollHeight: 600}, activeElement: null, addEventListener() {}, dispatchEvent() {}},
    CustomEvent: class {constructor(type) {this.type = type;}}, crypto: {randomUUID: () => 'stable-request'},
    setInterval: (fn) => { intervals.push(fn); return intervals.length; }, clearInterval() {}, setTimeout: () => 1, clearTimeout() {},
  });
  runInContext(source, context); await settle();
  return {root, requests, messages, listeners, parent, intervals,
    setOnline: (value) => { state.browser_online = value; },
    setStatus: (value) => { storeStatus = value; },
    setState: (value) => {state = structuredClone(value);},
    find: (tag, text) => root.querySelectorAll(tag).find((n) => n.textContent === text || n.attributes['aria-label'] === text),
  };
}

test('before setup is finished the widget shows what is missing and links to the setup page', async () => {
  const ui = await app();
  assert.match(ui.root.textContent, /Browser extension Not connected/);
  assert.match(ui.root.textContent, /Stores None chosen/);
  await ui.find('a', 'Finish setup ↗').listeners.click({preventDefault() {}}); await settle();
  assert.equal(ui.messages.find((m) => m.method === 'ui/open-link').params.url, 'http://127.0.0.1:24816/');
  const context = JSON.parse(ui.messages.find((m) => m.method === 'ui/update-model-context').params.content[0].text);
  assert.equal(context.setup_ready, false);
  assert.equal(context.setup_url, 'http://127.0.0.1:24816/');
});

test('the embedded app initializes the MCP Apps bridge and tells the model when setup is ready', async () => {
  const state = initial(); state.preferences = {draft: 'Vegetarian dinners for two', retailer_ids: ['willys']}; state.browser_online = true; state.stores[0].selected = true; state.stores[0].session = {status: 'ready', label: 'Willys Fältöversten'};
  const ui = await app({snapshot: state});
  assert.equal(ui.messages[0].method, 'ui/initialize');
  assert.equal(ui.messages[0].params.protocolVersion, '2026-01-26');
  assert.ok(ui.messages.some((m) => m.method === 'ui/notifications/initialized'));
  assert.match(ui.root.textContent, /Willys Willys Fältöversten/);
  assert.equal(ui.find('a', 'Finish setup ↗'), undefined);
  assert.equal(JSON.parse(ui.messages.find((m) => m.method === 'ui/update-model-context').params.content[0].text).setup_ready, true);
  ui.listeners.get('message')({source: {}, data: {jsonrpc: '2.0', method: 'ui/notifications/tool-result', params: {structuredContent: {...state, stores: [{name: 'Injected state'}]}}}});
  assert.equal(ui.root.textContent.includes('Injected state'), false);
});

test('a store that still needs delivery or pickup is checked again from the widget', async () => {
  const state = initial(); state.preferences = {draft: '', retailer_ids: ['willys']}; state.browser_online = true; state.stores[0].selected = true; state.stores[0].session = {status: 'setup_required'};
  const ui = await app({snapshot: state});
  assert.match(ui.root.textContent, /Willys Choose delivery or pickup/);
  ui.setStatus('ready');
  await ui.find('button', 'Check').click(); await settle();
  assert.deepEqual(ui.requests.at(-1), {name: 'connect_store', args: {retailer_id: 'willys'}});
  assert.match(ui.root.textContent, /Willys Willys Stockholm/);
  assert.equal(ui.find('a', 'Finish setup ↗'), undefined);
});

function savedBasket() {
  const state = initial();
  const context = {store_id: 'store-1', postal_code: '99999', fulfillment: 'delivery', delivery_slot_id: 'old-slot'};
  state.preferences = {draft: 'Mjölk', retailer_ids: ['willys']};
  state.browser_online = true;
  state.stores[0].selected = true;
  state.stores[0].session = {status: 'ready', context};
  state.plan = {id: 'saved-plan', revision: 16, text: 'Mjölk', retailer_id: 'willys', warnings: [], lines: [], baskets: [{retailer_id: 'willys', context, status: 'ready', missing: 0, over_budget: false, total: {amount_minor: 1490, currency: 'SEK'}}]};
  state.review_url = 'http://127.0.0.1:24816/?plan=saved-plan';
  return state;
}

test('a saved basket refreshes changed delivery settings and sends you to the page to fill it', async () => {
  const state = savedBasket();
  state.stores[0].session.context = {...state.stores[0].session.context, delivery_slot_id: 'new-slot'};
  const ui = await app({snapshot: state});
  assert.equal(ui.root.querySelectorAll('button').some((n) => /Fill/.test(n.textContent)), false);
  assert.equal(ui.find('a', 'Review and fill ↗').className, 'button small');
  const updated = structuredClone(state);
  updated.plan.baskets[0].context = updated.stores[0].session.context;
  updated.plan.revision++;
  ui.setState(updated);
  await ui.find('button', 'Refresh for this store').click(); await settle();
  assert.equal(ui.requests.at(-1).name, 'refresh_plan');
  assert.equal(ui.requests.at(-1).args.revision, 16);
  const review = ui.find('a', 'Review and fill ↗');
  assert.equal(review.className, 'button primary');
  await review.listeners.click({preventDefault() {}}); await settle();
  assert.equal(ui.messages.find((m) => m.method === 'ui/open-link').params.url, 'http://127.0.0.1:24816/?plan=saved-plan');
});

test('a saved basket whose store needs attention checks it in place', async () => {
  const state = savedBasket();
  state.stores[0].session.status = 'setup_required';
  const ui = await app({snapshot: state});
  ui.setStatus('ready');
  await ui.find('button', 'Check Willys').click(); await settle();
  assert.deepEqual(ui.requests.at(-1), {name: 'connect_store', args: {retailer_id: 'willys'}});
  assert.equal(ui.find('button', 'Check Willys'), undefined);
  assert.ok(ui.find('a', 'Review and fill ↗'));
  assert.equal(ui.requests.some((r) => r.name === 'create_shopping_plan'), false);
  assert.equal(ui.messages.some((m) => m.method === 'ui/message'), false);
});
