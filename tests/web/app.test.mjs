import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {createContext, runInContext} from 'node:vm';

const source = await readFile(new URL('../../internal/backend/web/app.js', import.meta.url), 'utf8');
const html = await readFile(new URL('../../internal/backend/web/index.html', import.meta.url), 'utf8');
const basketURL = 'https://www.mathem.se/se/cart/';
const shops = [
  {id: 'mathem', name: 'Mathem', mode: 'browser', basket_url: basketURL, context: {store_id: 'mathem', postal_code: '00000', fulfillment: ''}},
  {id: 'willys', name: 'Willys', mode: 'browser', basket_url: 'https://www.willys.se/varukorg', context: {store_id: 'willys', postal_code: '00000', fulfillment: ''}},
  {id: 'hemkop', name: 'Hemköp', mode: 'browser', basket_url: 'https://www.hemkop.se/varukorg', context: {store_id: 'hemkop', postal_code: '00000', fulfillment: ''}},
];
const demo = [{id: 'mathem', name: 'Mathem', mode: 'mock', basket_url: basketURL, context: {store_id: 'mathem-demo', postal_code: '11122', fulfillment: 'delivery', memberships: ['mathem-demo-member']}}];

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

async function app(retailers = shops) {
  const elements = new Map([...html.matchAll(/id="([^"]+)"/g)].map((match) => [match[1], new Element()]));
  elements.get('token').value = 'test-user';
  const events = new Map(), documentEvents = new Map(), timers = new Map(), navigation = [], requests = [];
  let now = 0, nextID = 1, uuid = 0;
  const responses = {
    '/v1/retailers': {retailers},
    '/v1/clients': {clients: [{id: 'demo-browser'}]},
    '/healthz': {interpreter: 'codex'},
    '/v1/shopping-plans': {plans: []},
  };
  const values = new Map();
  const storage = {getItem: (key) => values.get(key) || null, setItem: (key, value) => values.set(key, value), removeItem: (key) => values.delete(key)};
  const emit = (type, detail) => documentEvents.get(type)?.({detail: JSON.stringify(detail)});
  const context = createContext({
    document: {getElementById: (id) => elements.get(id), createElement: () => new Element(), querySelectorAll: () => [], addEventListener: (type, callback) => documentEvents.set(type, callback), dispatchEvent: (event) => documentEvents.get(event.type)?.(event)},
    window: {location: {assign: (url) => navigation.push(url)}, addEventListener: (event, callback) => events.set(event, callback)},
    localStorage: storage, sessionStorage: storage, CustomEvent: class { constructor(type) { this.type = type; } },
    crypto: {randomUUID: () => `key-${++uuid}`},
    fetch: async (path, options) => {
      assert.ok(responses[path], `Unexpected request ${path}`);
      requests.push({path, ...options, body: options.body ? JSON.parse(options.body) : undefined});
      return {ok: true, json: async () => responses[path]};
    },
    setTimeout: (callback, delay) => { const id = nextID++; timers.set(id, {callback, time: now + delay}); return id; },
    clearTimeout: (id) => timers.delete(id), setInterval: () => 1, clearInterval() {},
  });
  await runInContext(source, context);
  await new Promise((resolve) => setImmediate(resolve));
  return {
    elements, navigation, events, emit, requests, responses,
    choose: (shop) => { elements.get('retailer').value = shop; elements.get('retailer').listeners.change(); },
    receipt: (state, retailer = 'mathem', created = new Date().toISOString()) => runInContext(`showReceipt(${JSON.stringify({id: 'transfer', state, retailer_id: retailer, basket_url: basketURL, created_at: created, receipt: {lines: [{product_id: 'rice', status: 'unavailable', confirmed_quantity: 0, message: 'Not sold online at your Mathem store.'}]}})})`, context),
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

function shoppingPlan(retailer = 'mathem') {
  return {id: 'plan', revision: 1, retailer_id: retailer, status: 'ready', context: shops[0].context, warnings: [], total: {amount_minor: 2500, currency: 'SEK'}, lines: [
    {id: 'salad', item: {label: 'Sallad', query: 'sallad', amount: {quantity: 200, unit: 'g'}, required_traits: []}, candidates: [], issue: 'No products found.'},
    {id: 'rice', item: {label: 'Ris', query: 'ris', amount: {quantity: 500, unit: 'g'}, required_traits: []}, candidates: [], selected: {target_quantity: 1, product: {id: 'rice', name: 'Basmatiris', pack: {quantity: 500, unit: 'g'}}, unit_price: {amount_minor: 2500, currency: 'SEK'}, total: {amount_minor: 2500, currency: 'SEK'}}},
  ]};
}

test('missing groceries are visible and do not disable the remaining basket', async () => {
  const ui = await app();
  ui.emit('ship-it-extension-ready', {connected: true, client_id: 'browser'});
  ui.plan(shoppingPlan());
  assert.match(ui.elements.get('missing').textContent, /Not included in the basket.*Sallad/);
  assert.equal(ui.elements.get('transfer').disabled, false);
  assert.match(ui.elements.get('plan-state').textContent, /Ready/);
  assert.equal(ui.elements.get('plan-meta').textContent, 'Mathem');
});

test('every shop is selectable and a choice is required before finding groceries', async () => {
  const ui = await app();
  const picker = ui.elements.get('retailer');
  assert.deepEqual(picker.children.map((option) => option.textContent), ['Choose a shop', 'Mathem', 'Willys', 'Hemköp']);
  assert.deepEqual(picker.children.map((option) => Boolean(option.disabled)), [true, false, false, false]);
  assert.equal(picker.value, '');
  assert.equal(picker.disabled, false);
  assert.equal(ui.elements.get('create').disabled, true);
  assert.match(ui.elements.get('shop-status').textContent, /Choose a shop/);
  ui.elements.get('plan-form').listeners.submit({preventDefault() {}});
  assert.equal(ui.requests.some((request) => request.path === '/v1/shopping-plans' && request.method === 'POST'), false);
  assert.equal(picker.focused, true);
});

test('the chosen shop and its catalog are used for the grocery request and not carried over', async () => {
  const ui = await app();
  ui.choose('willys');
  assert.equal(ui.elements.get('create').disabled, false);
  assert.equal(ui.elements.get('shop-status').textContent, '');
  ui.responses['/v1/shopping-plans'] = {...shoppingPlan('willys'), plans: []};
  ui.elements.get('text').value = 'milk';
  ui.elements.get('plan-form').listeners.submit({preventDefault() {}});
  const request = ui.requests.find((request) => request.path === '/v1/shopping-plans' && request.method === 'POST');
  assert.equal(request.body.retailer_id, 'willys');
  assert.deepEqual(request.body.context, shops[1].context);
  const reloaded = await app();
  assert.equal(reloaded.elements.get('retailer').value, '');
  assert.equal(reloaded.elements.get('create').disabled, true);
});

test('filling a live basket asks for the extension until it connects', async () => {
  const ui = await app();
  ui.plan(shoppingPlan());
  assert.equal(ui.elements.get('transfer').disabled, true);
  assert.equal(ui.elements.get('extension-setup').hidden, false);
  assert.equal(ui.elements.get('extension-install').hidden, false);
  ui.emit('ship-it-extension-ready', {connected: false, error: 'Pairing failed.'});
  assert.match(ui.elements.get('extension-status').textContent, /Pairing failed/);
  assert.equal(ui.elements.get('extension-install').hidden, true);
  assert.equal(ui.elements.get('transfer').disabled, true);
  ui.emit('ship-it-extension-ready', {connected: true, client_id: 'browser'});
  assert.equal(ui.elements.get('extension-setup').hidden, true);
  assert.equal(ui.elements.get('transfer').disabled, false);
  ui.responses['/v1/shopping-plans/plan/transfer'] = {id: 'transfer', state: 'pending', retailer_id: 'mathem', basket_url: basketURL};
  ui.elements.get('transfer').listeners.click();
  await new Promise((resolve) => setImmediate(resolve));
  const request = ui.requests.find((request) => request.path === '/v1/shopping-plans/plan/transfer');
  assert.equal(request.body.client_id, 'browser');
  assert.match(ui.elements.get('receipt').textContent, /Opening Mathem in a new tab/);
});

test('demo shops are selectable and fill their basket without the extension', async () => {
  const ui = await app(demo);
  assert.deepEqual(ui.elements.get('retailer').children.map((option) => option.textContent), ['Choose a shop', 'Mathem · demo']);
  ui.choose('mathem');
  assert.equal(ui.elements.get('create').disabled, false);
  ui.plan({...shoppingPlan(), context: demo[0].context});
  assert.equal(ui.elements.get('extension-setup').hidden, true);
  assert.equal(ui.elements.get('transfer').disabled, false);
  assert.equal(ui.elements.get('plan-meta').textContent, 'Mathem · Delivery · Demo');
});

test('a filled live basket points to the shop tab and leaves this page in place', async () => {
  const ui = await app();
  ui.emit('ship-it-extension-ready', {connected: true, client_id: 'browser'});
  ui.plan(shoppingPlan());
  ui.receipt('partial');
  const receipt = ui.elements.get('receipt');
  assert.match(receipt.textContent, /Some items need your review in the Mathem tab/);
  assert.match(receipt.textContent, /Basmatiris: Unavailable · Not sold online at your Mathem store/);
  assert.equal(receipt.children.find((node) => node.href)?.href, basketURL);
  ui.advance(10000);
  assert.deepEqual(ui.navigation, []);
  const demoUI = await app(demo);
  demoUI.receipt('completed');
  assert.match(demoUI.elements.get('receipt').textContent, /Demo: Your basket is ready to review/);
});

test('a finished fill can be tried again as a new attempt', async () => {
  const ui = await app();
  ui.emit('ship-it-extension-ready', {connected: true, client_id: 'browser'});
  ui.plan(shoppingPlan());
  const fill = async (state) => {
    ui.responses['/v1/shopping-plans/plan/transfer'] = {id: `transfer-${state}`, state, retailer_id: 'mathem', basket_url: basketURL, created_at: new Date().toISOString()};
    ui.elements.get('transfer').listeners.click();
    await new Promise((resolve) => setImmediate(resolve));
    return ui.requests.filter((request) => request.path === '/v1/shopping-plans/plan/transfer').at(-1).headers['Idempotency-Key'];
  };
  const first = await fill('failed');
  assert.match(ui.elements.get('receipt').textContent, /could not be updated/);
  const second = await fill('expired');
  assert.notEqual(second, first);
  assert.match(ui.elements.get('receipt').textContent, /did not pick this up/);
  const third = await fill('pending');
  assert.notEqual(third, second);
  assert.equal(await fill('pending'), third);
});

test('a fill nobody picks up explains that the extension is not responding', async () => {
  const ui = await app();
  ui.plan(shoppingPlan());
  ui.receipt('pending');
  assert.match(ui.elements.get('receipt').textContent, /Opening Mathem in a new tab/);
  ui.receipt('pending', 'mathem', new Date(Date.now() - 15000).toISOString());
  assert.match(ui.elements.get('receipt').textContent, /Waiting for the Ship It extension/);
  ui.receipt('expired');
  assert.match(ui.elements.get('receipt').textContent, /did not pick this up/);
  assert.match(ui.elements.get('extension-status').textContent, /not running in this browser/);
});
