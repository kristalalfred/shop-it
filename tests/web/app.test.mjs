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
  querySelector() { return this; }
  scrollIntoView() {}
  focus() { this.focused = true; }
}

async function app(mode = 'browser', {shops, sessions} = {}) {
  const elements = new Map([...html.matchAll(/id="([^"]+)"/g)].map((match) => [match[1], new Element()]));
  elements.get('token').value = 'test-user';
  const events = new Map(), documentEvents = new Map(), timers = new Map(), navigation = [], requests = [];
  let now = 0, nextID = 1;
  const responses = {
    '/v1/retailers': {retailers: shops || [{id: 'mathem', name: 'Mathem', mode}]},
    '/v1/clients': {clients: [{id: 'browser'}]},
    '/healthz': {interpreter: 'codex'},
    '/v1/browser-sessions': {sessions: sessions || [{retailer_id: 'mathem', client_id: 'browser', context: store, logged_in: true}], issues: []},
    '/v1/shopping-plans': {plans: []},
  };
  const values = new Map();
  const storage = {getItem: (key) => values.get(key) || null, setItem: (key, value) => values.set(key, value), removeItem: (key) => values.delete(key)};
  const emit = (type, detail) => documentEvents.get(type)?.({detail: JSON.stringify(detail)});
  const context = createContext({
    document: {getElementById: (id) => elements.get(id), createElement: () => new Element(), querySelectorAll: () => [], addEventListener: (type, callback) => documentEvents.set(type, callback), dispatchEvent: (event) => documentEvents.get(event.type)?.(event)},
    window: {location: {assign: (url) => navigation.push(url)}, addEventListener: (event, callback) => events.set(event, callback)},
    localStorage: storage, sessionStorage: storage, CustomEvent: class { constructor(type) { this.type = type; } },
    fetch: async (path, options) => {
      assert.ok(responses[path], `Unexpected request ${path}`);
      requests.push({path, ...options, body: options.body ? JSON.parse(options.body) : undefined});
      return {ok: true, json: async () => responses[path]};
    },
    setTimeout: (callback, delay) => { const id = nextID++; timers.set(id, {callback, time: now + delay}); return id; },
    clearTimeout: (id) => timers.delete(id), setInterval: () => 1, clearInterval() {},
  });
  await runInContext(source, context);
  return {
    elements, navigation, events, emit, requests, storage, responses,
    refresh: () => runInContext('refreshSessions()', context),
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

const shops = [
  {id: 'mathem', name: 'Mathem', mode: 'browser', basket_url: basketURL},
  {id: 'willys', name: 'Willys', mode: 'browser', basket_url: 'https://www.willys.se/'},
  {id: 'hemkop', name: 'Hemköp', mode: 'browser', basket_url: 'https://www.hemkop.se/'},
];
const mathem = {retailer_id: 'mathem', client_id: 'browser', context: store, logged_in: true};
const willys = {retailer_id: 'willys', client_id: 'browser', context: {...store, store_id: 'willys'}, logged_in: true};

function choose(ui, shop) {
  const picker = ui.elements.get('retailer');
  picker.value = shop; picker.listeners.change();
}

test('shop picker lists every shop and requires a choice before finding groceries', async () => {
  const ui = await app('browser', {shops, sessions: [mathem, willys]});
  const picker = ui.elements.get('retailer');
  assert.deepEqual(picker.children.map((option) => option.textContent), ['Choose a shop', 'Mathem', 'Willys', 'Hemköp · not connected']);
  assert.deepEqual(picker.children.map((option) => Boolean(option.disabled)), [true, false, false, true]);
  assert.equal(picker.value, '');
  assert.equal(ui.elements.get('create').disabled, true);
  assert.match(ui.elements.get('shop-status').textContent, /Choose a shop/);
  assert.equal(ui.elements.get('connection-summary').textContent, 'Shops');
  assert.equal(ui.elements.get('add-shop').textContent, 'Connect another shop');
  assert.equal(ui.elements.get('extension-setup').hidden, true);
  assert.deepEqual(ui.elements.get('retailer-links').children.map((link) => link.textContent), ['Hemköp']);
  ui.elements.get('plan-form').listeners.submit({preventDefault() {}});
  assert.equal(ui.requests.some((request) => request.path === '/v1/shopping-plans' && request.method === 'POST'), false);
  assert.equal(picker.focused, true);
});

test('chosen shop is used for the grocery request and not carried over to the next visit', async () => {
  const ui = await app('browser', {shops, sessions: [mathem, willys]});
  choose(ui, 'willys');
  assert.equal(ui.elements.get('create').disabled, false);
  assert.equal(ui.elements.get('shop-status').textContent, '');
  ui.responses['/v1/shopping-plans'] = {...shoppingPlan(), retailer_id: 'willys', plans: []};
  ui.elements.get('text').value = 'milk';
  ui.elements.get('plan-form').listeners.submit({preventDefault() {}});
  const request = ui.requests.find((request) => request.path === '/v1/shopping-plans' && request.method === 'POST');
  assert.equal(request.body.retailer_id, 'willys');
  assert.deepEqual(request.body.context, willys.context);
  const reloaded = await app('browser', {shops, sessions: [mathem, willys]});
  assert.equal(reloaded.elements.get('retailer').value, '');
  assert.equal(reloaded.elements.get('create').disabled, true);
  const anonymous = await app('browser', {shops, sessions: [{...willys, logged_in: false}]});
  choose(anonymous, 'willys');
  assert.equal(anonymous.elements.get('create').disabled, false);
  assert.match(anonymous.elements.get('shop-status').textContent, /Sign in at Willys before filling/);
});

test('losing the selected shop never switches to another connected retailer', async () => {
  const ui = await app('browser', {shops, sessions: [mathem, willys]});
  choose(ui, 'willys');
  const originalOptions = ui.elements.get('retailer').children;
  await ui.refresh();
  assert.equal(ui.elements.get('retailer').children, originalOptions);
  ui.responses['/v1/browser-sessions'] = {sessions: [mathem], issues: []};
  await ui.refresh();
  assert.equal(ui.elements.get('retailer').value, '');
  assert.equal(ui.elements.get('create').disabled, true);
  assert.match(ui.elements.get('shop-status').textContent, /Willys disconnected/);
  ui.elements.get('plan-form').listeners.submit({preventDefault() {}});
  assert.equal(ui.requests.some((request) => request.path === '/v1/shopping-plans' && request.method === 'POST'), false);
  ui.responses['/v1/browser-sessions'] = {sessions: [mathem, willys], issues: []};
  await ui.refresh();
  assert.equal(ui.elements.get('retailer').value, 'willys');
  assert.equal(ui.elements.get('create').disabled, false);
});

test('shops that fail to connect explain why under the shop picker', async () => {
  const ui = await app('browser', {shops, sessions: [mathem]});
  assert.deepEqual(ui.elements.get('shop-issues').children, []);
  ui.responses['/v1/browser-sessions'] = {sessions: [mathem], issues: [
    {retailer_id: 'willys', client_id: 'browser', message: 'Select a store and delivery or pickup on Willys.'},
    {retailer_id: 'mathem', client_id: 'other', message: 'Reload your Mathem tab to connect it.'},
  ]};
  await ui.refresh();
  assert.deepEqual(ui.elements.get('shop-issues').children.map((line) => line.textContent), ["Willys isn't connected. Select a store and delivery or pickup on Willys."]);
  ui.responses['/v1/browser-sessions'] = {sessions: [mathem, willys], issues: []};
  await ui.refresh();
  assert.deepEqual(ui.elements.get('shop-issues').children, []);
});

test('paired extension hides setup even without an open shop', async () => {
  const ui = await app('browser', {shops, sessions: []});
  assert.equal(ui.elements.get('create').disabled, true);
  assert.match(ui.elements.get('shop-status').textContent, /Connect a shop/);
  assert.equal(ui.elements.get('shop-setup').open, true);
  ui.emit('ship-it-extension-ready', {connected: true});
  assert.equal(ui.elements.get('extension-setup').hidden, true);
  assert.equal(ui.elements.get('shop-links').hidden, false);
  assert.deepEqual(ui.elements.get('retailer-links').children.map((link) => link.href), shops.map((shop) => shop.basket_url));
  assert.equal(ui.elements.get('add-shop').textContent, 'Connect a shop');
});

test('extension pairing distinguishes installed, failed and connected states', async () => {
  const ui = await app('browser', {shops, sessions: []});
  assert.equal(ui.elements.get('pair-extension').disabled, true);
  ui.emit('ship-it-extension-ready', {connected: false});
  assert.equal(ui.elements.get('pair-extension').disabled, false);
  assert.equal(ui.elements.get('extension-install').hidden, true);
  ui.elements.get('pair-extension').listeners.click();
  assert.equal(ui.elements.get('pair-extension').disabled, true);
  ui.emit('ship-it-paired', {error: 'Pairing failed'});
  assert.equal(ui.elements.get('extension-setup').hidden, false);
  assert.equal(ui.elements.get('pair-extension').disabled, false);
  assert.match(ui.elements.get('extension-status').textContent, /Pairing failed/);
  ui.emit('ship-it-paired', {client_id: 'browser'});
  assert.equal(ui.elements.get('extension-setup').hidden, true);
  assert.equal(ui.elements.get('shop-links').hidden, false);
});

test('demo shops remain selectable without extension setup', async () => {
  const ui = await app('mock');
  assert.equal(ui.elements.get('shop-setup').hidden, true);
  assert.deepEqual(ui.elements.get('retailer').children.map((option) => option.textContent), ['Choose a shop', 'Mathem · demo']);
  choose(ui, 'mathem');
  assert.equal(ui.elements.get('create').disabled, false);
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
