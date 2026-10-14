import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {createContext, runInContext} from 'node:vm';

const source = await readFile(new URL('../../internal/backend/web/app.js', import.meta.url), 'utf8');
const html = await readFile(new URL('../../internal/backend/web/index.html', import.meta.url), 'utf8');
const store = {store_id: 'mathem', postal_code: '11122', fulfillment: 'delivery', session_id: 'test', memberships: []};
const basketURL = 'https://www.mathem.se/se/cart/';

class Element {
  constructor() { this.children = []; this.listeners = {}; this.value = ''; this.checked = false; this.hidden = false; this.text = ''; }
  set textContent(value) { this.text = value; this.children = []; }
  get textContent() { return this.text + this.children.map((node) => node.textContent).join(' '); }
  append(...nodes) { this.children.push(...nodes); }
  replaceChildren(...nodes) { this.text = ''; this.children = nodes; }
  addEventListener(name, callback) { this.listeners[name] = callback; }
}

async function app(mode = 'browser') {
  const elements = new Map([...html.matchAll(/id="([^"]+)"/g)].map((match) => [match[1], new Element()]));
  elements.get('token').value = 'test-user';
  const events = new Map(), timers = new Map(), navigation = [];
  let now = 0, nextID = 1;
  const responses = {
    '/v1/retailers': {retailers: [{id: 'mathem', name: 'Mathem', mode}]},
    '/v1/clients': {clients: [{id: 'browser'}]},
    '/healthz': {interpreter: 'codex'},
    '/v1/browser-sessions': {sessions: [{retailer_id: 'mathem', client_id: 'browser', context: store, logged_in: true}]},
    '/v1/shopping-plans': {plans: []},
  };
  const storage = {getItem: () => null, setItem() {}, removeItem() {}};
  const context = createContext({
    document: {getElementById: (id) => elements.get(id), createElement: () => new Element(), querySelectorAll: () => [], addEventListener() {}, dispatchEvent() {}},
    window: {location: {assign: (url) => navigation.push(url)}, addEventListener: (event, callback) => events.set(event, callback)},
    localStorage: storage, sessionStorage: storage, CustomEvent: class {},
    fetch: async (path) => {
      assert.ok(responses[path], `Unexpected request ${path}`);
      return {ok: true, json: async () => responses[path]};
    },
    setTimeout: (callback, delay) => { const id = nextID++; timers.set(id, {callback, time: now + delay}); return id; },
    clearTimeout: (id) => timers.delete(id), setInterval: () => 1, clearInterval() {},
  });
  await runInContext(source, context);
  return {
    elements, navigation, events,
    receipt: (state, id = 'transfer') => runInContext(`showReceipt(${JSON.stringify({id, state, retailer_id: 'mathem', basket_url: basketURL, receipt: {lines: []}})})`, context),
    plan: (value) => runInContext(`showPlan(${JSON.stringify(value)})`, context),
    advance: (duration) => {
      const end = now + duration;
      for (;;) {
        const entry = [...timers.entries()].sort((a, b) => a[1].time - b[1].time).find(([, timer]) => timer.time <= end);
        if (!entry) break;
        const [id, timer] = entry;
        timers.delete(id); now = timer.time; timer.callback();
      }
      now = end;
    },
  };
}

function shoppingPlan() {
  return {id: 'plan', retailer_id: 'mathem', status: 'ready', context: store, warnings: [], total: {amount_minor: 2500, currency: 'SEK'}, lines: [
    {id: 'salad', item: {label: 'Sallad', query: 'sallad', amount: {quantity: 200, unit: 'g'}, required_traits: []}, candidates: [], issue: 'No products found.'},
    {id: 'rice', item: {label: 'Ris', query: 'ris', amount: {quantity: 500, unit: 'g'}, required_traits: []}, candidates: [], selected: {target_quantity: 1, product: {name: 'Basmatiris', pack: {quantity: 500, unit: 'g'}}, unit_price: {amount_minor: 2500, currency: 'SEK'}, total: {amount_minor: 2500, currency: 'SEK'}}},
  ]};
}

test('missing groceries are visible and do not disable the remaining basket', async () => {
  const ui = await app();
  ui.plan(shoppingPlan());
  assert.match(ui.elements.get('missing').textContent, /Not included in the basket.*Sallad/);
  assert.equal(ui.elements.get('transfer').disabled, false);
  assert.match(ui.elements.get('plan-state').textContent, /Ready/);
});

test('completed live basket opens in the same page after three seconds', async () => {
  const ui = await app();
  ui.receipt('completed');
  assert.match(ui.elements.get('receipt').textContent, /3 seconds/);
  ui.advance(2999);
  assert.deepEqual(ui.navigation, []);
  ui.advance(1);
  assert.deepEqual(ui.navigation, [basketURL]);
});

test('repeated completion updates do not postpone navigation', async () => {
  const ui = await app();
  ui.receipt('completed');
  ui.advance(2000);
  ui.receipt('completed');
  ui.advance(1000);
  assert.deepEqual(ui.navigation, [basketURL]);
});

test('pending, partial, failed and demo baskets do not redirect', async () => {
  const ui = await app();
  for (const state of ['pending', 'claimed', 'partial', 'failed', 'uncertain', 'login_required']) {
    ui.receipt(state); ui.advance(4000);
  }
  assert.deepEqual(ui.navigation, []);
  const demo = await app('mock');
  demo.receipt('completed'); demo.advance(4000);
  assert.deepEqual(demo.navigation, []);
});

test('opening another plan or leaving the page cancels a pending redirect', async () => {
  const ui = await app();
  ui.receipt('completed'); ui.advance(1000);
  ui.plan(shoppingPlan()); ui.advance(3000);
  assert.deepEqual(ui.navigation, []);
  ui.receipt('completed', 'another-transfer');
  ui.events.get('pagehide')(); ui.advance(4000);
  assert.deepEqual(ui.navigation, []);
});
