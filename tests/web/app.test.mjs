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
  constructor() { this.children = []; this.listeners = {}; this.attributes = {}; this.value = ''; this.checked = false; this.hidden = false; this.text = ''; }
  set textContent(value) { this.text = value; this.children = []; }
  get textContent() { return this.text + this.children.map((node) => node.textContent).join(' '); }
  append(...nodes) { this.children.push(...nodes); }
  replaceChildren(...nodes) { this.text = ''; this.children = nodes; }
  addEventListener(name, callback) { this.listeners[name] = callback; }
  setAttribute(name, value) { this.attributes[name] = value; }
  querySelector() { return this; }
  scrollIntoView() {}
  focus() { this.focused = true; }
}

async function app(retailers = shops) {
  const elements = new Map([...html.matchAll(/id="([^"]+)"/g)].map((match) => [match[1], new Element()]));
  elements.get('token').value = 'test-user';
  const events = new Map(), documentEvents = new Map(), timers = new Map(), navigation = [], requests = [];
  let now = 0, nextID = 1, uuid = 0, stream = null;
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
    window: {location: {assign: (url) => navigation.push(url)}, addEventListener: (event, callback) => events.set(event, callback), scrollTo() {}},
    localStorage: storage, sessionStorage: storage, CustomEvent: class { constructor(type) { this.type = type; } }, TextDecoder,
    crypto: {randomUUID: () => `key-${++uuid}`},
    fetch: async (path, options) => {
      assert.ok(responses[path], `Unexpected request ${path}`);
      requests.push({path, ...options, body: options.body ? JSON.parse(options.body) : undefined});
      if (options.headers.Accept === 'application/x-ndjson') return {ok: true, body: new ReadableStream({start: (controller) => { stream = controller; }})};
      return {ok: true, json: async () => responses[path]};
    },
    setTimeout: (callback, delay) => { const id = nextID++; timers.set(id, {callback, time: now + delay}); return id; },
    clearTimeout: (id) => timers.delete(id), setInterval: () => 1, clearInterval() {},
  });
  await runInContext(source, context);
  await new Promise((resolve) => setImmediate(resolve));
  return {
    elements, navigation, events, emit, requests, responses,
    send: async (event) => { stream.enqueue(new TextEncoder().encode(`${JSON.stringify(event)}\n`)); await settle(); },
    receipt: (state, retailer = 'mathem', created = new Date().toISOString(), lines = [{product_id: 'rice', status: 'unavailable', confirmed_quantity: 0, message: 'Not sold online at your Mathem store.'}]) => runInContext(`showFill(${JSON.stringify({id: 'transfer', state, retailer_id: retailer, basket_url: basketURL, created_at: created, receipt: {lines}})})`, context),
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

async function settle() {
  for (let i = 0; i < 5; i++) await new Promise((resolve) => setImmediate(resolve));
}

const classes = (element) => element.children.map((node) => node.className);
const texts = (element) => element.children.map((node) => node.textContent);

const sek = (amount_minor) => ({amount_minor, currency: 'SEK'});
const rice = (name, price) => ({candidates: [], selected: {eligible: true, target_quantity: 1, product: {id: 'rice', name, pack: {quantity: 500, unit: 'g'}}, unit_price: sek(price), total: sek(price)}});
const notFound = {candidates: [], issue: 'No products found.'};

function shoppingPlan(retailer = 'mathem') {
  return {id: 'plan', revision: 1, retailer_id: retailer, warnings: [], budget_minor: 0, baskets: [
    {retailer_id: 'mathem', context: shops[0].context, status: 'ready', total: sek(2500), missing: 1},
    {retailer_id: 'willys', context: shops[1].context, status: 'ready', total: sek(2900), missing: 1},
  ], lines: [
    {id: 'salad', item: {label: 'Sallad', query: 'sallad', amount: {quantity: 200, unit: 'g'}, required_traits: []}, skipped: false, baskets: {mathem: notFound, willys: notFound}},
    {id: 'rice', item: {label: 'Ris', query: 'ris', amount: {quantity: 500, unit: 'g'}, required_traits: []}, skipped: false, baskets: {mathem: rice('Basmatiris', 2500), willys: rice('Jasminris', 2900)}},
  ]};
}

function demoPlan() {
  const plan = shoppingPlan();
  plan.baskets = [{...plan.baskets[0], context: demo[0].context}];
  return plan;
}

test('missing groceries are visible and do not disable the remaining basket', async () => {
  const ui = await app();
  ui.emit('ship-it-extension-ready', {connected: true, client_id: 'browser'});
  ui.plan(shoppingPlan());
  assert.match(ui.elements.get('missing').textContent, /1 item needs attention/);
  assert.match(ui.elements.get('lines').textContent, /Sallad 200 g · No products found\. Not found/);
  assert.equal(ui.elements.get('transfer').disabled, false);
  assert.match(ui.elements.get('plan-state').textContent, /Ready/);
});

test('a requirement no product meets offers the closest alternative to choose', async () => {
  const ui = await app();
  const milk = (id, name, total, extra) => ({product: {id, name, pack: {quantity: 1000, unit: 'ml'}}, target_quantity: 2, unit_price: sek(total / 2), total: sek(total), eligible: true, reasons: [], ...extra});
  const alternative = milk('milk-basic', 'Mjölk standard 1 l', 2580, {unmet_traits: ['lactose_free', 'organic']});
  const plan = shoppingPlan();
  plan.lines[0] = {id: 'milk', item: {label: 'Mjölk', query: 'mjölk', amount: {quantity: 2000, unit: 'ml'}, required_traits: ['lactose_free', 'organic']}, skipped: false, baskets: {
    mathem: {issue: 'No product meets every requirement.', candidates: [alternative, milk('milk-lactose-free', 'Mjölk laktosfri 1,5 l', 4580, {eligible: false, unmet_traits: ['organic'], reasons: ['Availability: out_of_stock']})]},
    willys: notFound,
  }};
  ui.plan(plan);
  const lines = ui.elements.get('lines');
  assert.match(lines.textContent, /Mjölk 2000 ml · lactose free, organic · Closest: Mjölk standard 1 l · Not marked lactose free or organic Check/);
  const editor = lines.children[1].children[1];
  const [choice] = editor.children.filter((node) => node.className === 'candidate');
  assert.match(choice.textContent, /Mjölk standard 1 l 2 × 1000 ml · 25,80\skr · Not marked lactose free or organic Choose/);
  assert.match(editor.children.find((node) => node.className === 'others').textContent, /Mjölk laktosfri 1,5 l 2 × 1000 ml · 45,80\skr · Not marked organic · Availability: out_of_stock Choose/);
  const choose = choice.children[1];
  assert.equal(choose.disabled, false);
  const chosen = structuredClone(plan);
  chosen.revision = 2;
  chosen.lines[0].baskets.mathem.selected = alternative;
  ui.responses['/v1/shopping-plans/plan/lines/milk'] = chosen;
  choose.listeners.click();
  await settle();
  assert.deepEqual(ui.requests.find((request) => request.path === '/v1/shopping-plans/plan/lines/milk').body, {revision: 1, product_id: 'milk-basic'});
  assert.deepEqual(classes(lines), ['line', 'line']);
  assert.match(lines.textContent, /Mjölk 2000 ml · lactose free, organic · 2 × Mjölk standard 1 l · Not marked lactose free or organic/);
});

test('groceries are searched at every shop without choosing one first', async () => {
  const ui = await app();
  assert.equal(ui.elements.get('create').disabled, false);
  assert.equal(ui.elements.get('connection').textContent, '');
  ui.elements.get('text').value = 'milk';
  ui.elements.get('plan-form').listeners.submit({preventDefault() {}});
  await settle();
  const request = ui.requests.find((request) => request.path === '/v1/shopping-plans' && request.method === 'POST');
  assert.equal(request.headers.Accept, 'application/x-ndjson');
  assert.deepEqual(request.body, {text: 'milk', budget_minor: 0});
  assert.match(texts(ui.elements.get('progress-steps'))[1], /Search Mathem, Willys, and Hemköp$/);
});

test('each shop basket is a card and choosing one switches the list, total and fill target', async () => {
  const ui = await app();
  ui.emit('ship-it-extension-ready', {connected: true, client_id: 'browser'});
  ui.plan(shoppingPlan());
  const cards = () => ui.elements.get('baskets').children;
  assert.equal(ui.elements.get('shops').hidden, false);
  assert.match(cards()[0].textContent, /^Mathem 25,00\skr 1 missing$/);
  assert.match(cards()[1].textContent, /^Willys 29,00\skr 1 missing$/);
  assert.deepEqual(cards().map((card) => card.attributes['aria-pressed']), ['true', 'false']);
  assert.equal(ui.elements.get('list-count').textContent, '2 items · Mathem');
  assert.equal(ui.elements.get('transfer').textContent, 'Fill Mathem basket');
  cards()[0].listeners.click();
  await settle();
  assert.equal(ui.requests.some((request) => request.method === 'PATCH'), false);
  ui.responses['/v1/shopping-plans/plan'] = {...shoppingPlan('willys'), revision: 2};
  ui.responses['/v1/shopping-plans'] = {plans: [{...shoppingPlan('willys'), text: 'Risotto'}]};
  cards()[1].listeners.click();
  await settle();
  const choice = ui.requests.find((request) => request.path === '/v1/shopping-plans/plan');
  assert.equal(choice.method, 'PATCH');
  assert.deepEqual(choice.body, {revision: 1, retailer_id: 'willys'});
  assert.deepEqual(cards().map((card) => card.attributes['aria-pressed']), ['false', 'true']);
  assert.match(ui.elements.get('lines').textContent, /Ris 500 g · 1 × Jasminris/);
  assert.match(ui.elements.get('total').textContent, /29,00/);
  assert.equal(ui.elements.get('list-count').textContent, '2 items · Willys');
  assert.equal(ui.elements.get('transfer').textContent, 'Fill Willys basket');
  assert.match(ui.elements.get('history').textContent, /Risotto · Willys 29,00\skr/);
  ui.responses['/v1/shopping-plans/plan/transfer'] = {id: 'transfer', state: 'pending', retailer_id: 'willys', basket_url: 'https://www.willys.se/varukorg', created_at: new Date().toISOString()};
  ui.elements.get('transfer').listeners.click();
  await settle();
  const fill = ui.requests.find((request) => request.path === '/v1/shopping-plans/plan/transfer');
  assert.equal(fill.body.revision, 2);
  assert.match(ui.elements.get('receipt').textContent, /Opening Willys in a new tab/);
});

test('a plan from a single shop shows no shop choice', async () => {
  const ui = await app();
  const plan = shoppingPlan();
  plan.baskets = plan.baskets.slice(0, 1);
  ui.plan(plan);
  assert.equal(ui.elements.get('shops').hidden, true);
  assert.equal(ui.elements.get('list-count').textContent, '2 items · Mathem');
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

test('demo shops fill their basket without the extension', async () => {
  const ui = await app(demo);
  assert.equal(ui.elements.get('create').disabled, false);
  ui.plan(demoPlan());
  assert.equal(ui.elements.get('extension-setup').hidden, true);
  assert.equal(ui.elements.get('transfer').disabled, false);
  assert.equal(ui.elements.get('transfer').textContent, 'Fill Mathem demo basket');
  assert.match(ui.elements.get('estimate').textContent, /Demo prices/);
});

test('a partial fill marks the items that were not added in the list and leaves this page in place', async () => {
  const ui = await app();
  ui.emit('ship-it-extension-ready', {connected: true, client_id: 'browser'});
  ui.plan(shoppingPlan());
  ui.receipt('partial');
  const receipt = ui.elements.get('receipt');
  assert.match(receipt.textContent, /Some items were not added\. They are at the top of the list/);
  assert.doesNotMatch(receipt.textContent, /Basmatiris/);
  assert.equal(receipt.children.some((node) => node.href), false);
  assert.match(ui.elements.get('lines').textContent, /Ris 500 g · 1 × Basmatiris · Not sold online at your Mathem store\. Unavailable/);
  assert.equal(ui.elements.get('transfer').textContent, 'Fill basket again');
  assert.equal(ui.elements.get('new-list').hidden, false);
  assert.equal(ui.elements.get('refresh').hidden, true);
  ui.advance(10000);
  assert.deepEqual(ui.navigation, []);
});

test('a filled basket only confirms the fill and New list starts over', async () => {
  const ui = await app();
  ui.emit('ship-it-extension-ready', {connected: true, client_id: 'browser'});
  ui.elements.get('text').value = 'Korvstroganoff';
  ui.plan(shoppingPlan());
  ui.receipt('pending');
  assert.equal(ui.elements.get('transfer').textContent, 'Filling…');
  assert.equal(ui.elements.get('transfer').disabled, true);
  assert.equal(ui.elements.get('new-list').hidden, true);
  ui.receipt('completed', 'mathem', new Date().toISOString(), [{product_id: 'rice', status: 'applied', confirmed_quantity: 1}]);
  assert.deepEqual(ui.elements.get('receipt').children, []);
  assert.equal(ui.elements.get('filled').hidden, false);
  assert.equal(ui.elements.get('filled').textContent, '✓ Basket filled at Mathem');
  assert.equal(ui.elements.get('transfer').hidden, false);
  assert.equal(ui.elements.get('transfer').disabled, false);
  assert.equal(ui.elements.get('transfer').textContent, 'Fill basket again');
  assert.equal(ui.elements.get('transfer').className, '');
  assert.equal(ui.elements.get('refresh').hidden, true);
  assert.equal(ui.elements.get('new-list').className, 'primary');
  assert.match(ui.elements.get('lines').textContent, /Ris 500 g · 1 × Basmatiris/);
  assert.doesNotMatch(ui.elements.get('lines').textContent, /Unavailable|Not added/);
  ui.elements.get('new-list').listeners.click();
  assert.equal(ui.elements.get('review').hidden, true);
  assert.equal(ui.elements.get('checkout').hidden, true);
  assert.deepEqual(ui.elements.get('lines').children, []);
  assert.equal(ui.elements.get('list').className, 'list-panel empty');
  assert.equal(ui.elements.get('text').value, '');
  assert.equal(ui.elements.get('text').focused, true);
  assert.equal(ui.elements.get('create').disabled, false);
  const demoUI = await app(demo);
  demoUI.plan(demoPlan());
  demoUI.receipt('completed', 'mathem', new Date().toISOString(), []);
  assert.equal(demoUI.elements.get('filled').textContent, '✓ Demo basket filled');
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
  const filled = await fill('completed');
  assert.equal(ui.elements.get('transfer').textContent, 'Fill basket again');
  const first = await fill('failed');
  assert.notEqual(first, filled);
  assert.match(ui.elements.get('receipt').textContent, /could not be updated/);
  assert.equal(ui.elements.get('transfer').className, 'primary');
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

test('finding groceries shows each streamed step and then the list with problems first', async () => {
  const ui = await app();
  ui.elements.get('text').value = 'Korvstroganoff för 5';
  ui.elements.get('plan-form').listeners.submit({preventDefault() {}});
  await settle();
  const steps = ui.elements.get('progress-steps');
  assert.equal(ui.elements.get('progress').hidden, false);
  assert.equal(ui.elements.get('create').disabled, true);
  assert.deepEqual(classes(steps), ['step active', 'step pending', 'step pending']);
  await ui.send({progress: {stage: 'interpret', note: 'Choosing a stroganoff recipe'}});
  assert.match(texts(steps)[0], /Plan groceries Choosing a stroganoff recipe/);
  await ui.send({progress: {stage: 'search', total: 2}});
  await ui.send({progress: {stage: 'search', done: 1, total: 2}});
  assert.deepEqual(classes(steps), ['step done', 'step active', 'step pending']);
  assert.match(texts(steps)[0], /2 groceries/);
  assert.match(texts(steps)[1], /Search Mathem, Willys, and Hemköp 1 of 2 groceries/);
  await ui.send({progress: {stage: 'match'}});
  assert.deepEqual(classes(steps), ['step done', 'step done', 'step active']);
  const plan = shoppingPlan('willys');
  plan.lines.reverse();
  plan.warnings = ['Korvstroganoff för 5 portioner.'];
  await ui.send({plan});
  assert.equal(ui.elements.get('progress').hidden, true);
  assert.equal(ui.elements.get('create').disabled, false);
  const lines = ui.elements.get('lines');
  assert.deepEqual(classes(lines), ['group', 'line attention', 'group', 'line']);
  assert.deepEqual(texts(lines).map((text) => text.split(' ')[0]), ['Needs', 'Sallad', 'Matched', 'Ris']);
  assert.equal(ui.elements.get('list-count').textContent, '2 items · Willys');
  assert.match(ui.elements.get('notes').textContent, /Assumptions Korvstroganoff för 5 portioner/);
  assert.equal(ui.elements.get('ack-wrap').hidden, false);
});

test('a failed planning stream explains the failure and hides the progress', async () => {
  const ui = await app();
  ui.elements.get('text').value = 'milk';
  ui.elements.get('plan-form').listeners.submit({preventDefault() {}});
  await settle();
  await ui.send({progress: {stage: 'interpret'}});
  await ui.send({status: 502, error: 'adapter unavailable'});
  assert.equal(ui.elements.get('progress').hidden, true);
  assert.match(ui.elements.get('message').textContent, /did not respond/);
  assert.deepEqual(ui.elements.get('lines').children, []);
});
