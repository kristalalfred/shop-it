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
];
const demo = [{id: 'mathem', name: 'Mathem', mode: 'mock', basket_url: basketURL, context: {store_id: 'mathem-demo', postal_code: '11122', fulfillment: 'delivery', memberships: ['mathem-demo-member']}}];

class Element {
  constructor(tag = 'div') { this.tagName = tag; this.children = []; this.listeners = {}; this.attributes = {}; this.value = ''; this.checked = false; this.hidden = false; this.text = ''; this.className = ''; }
  set textContent(value) { this.text = value; this.children = []; }
  get textContent() { return this.text + this.children.map((node) => node.textContent).join(' '); }
  append(...nodes) { this.children.push(...nodes); }
  replaceChildren(...nodes) { this.text = ''; this.children = nodes; }
  addEventListener(name, callback) { this.listeners[name] = callback; }
  setAttribute(name, value) { this.attributes[name] = value; if (name === 'hidden') this.hidden = true; }
  removeAttribute(name) { delete this.attributes[name]; if (name === 'hidden') this.hidden = false; }
}

const setupState = (retailers = shops, extra = {}) => ({
  stores: retailers.map((r) => ({id: r.id, name: r.name, url: r.basket_url, mode: r.mode, selected: false})),
  browser_online: false, chatgpt_connected: false, setup_url: 'http://127.0.0.1:24816/', demo: retailers.every((r) => r.mode === 'mock'),
  install_url: 'codex://plugins/shop-it?marketplacePath=%2Fplugin', chat_url: 'codex://new?path=%2Fplugin&prompt=Shop%20It',
  extension_dir: '/Users/me/Library/Application Support/shop-it/extension', ...extra,
});
const find = (element, predicate) => predicate(element) ? element : element.children.map((child) => find(child, predicate)).find(Boolean);
const sek = (amount_minor) => ({amount_minor, currency: 'SEK'});
const rice = (name, price, extra = {}) => ({candidates: [], selected: {eligible: true, target_quantity: 1, product: {id: 'rice', name, pack: {quantity: 500, unit: 'g'}}, unit_price: sek(price), total: sek(price), ...extra}});
const notFound = {candidates: [], issue: 'No products found.'};

function shoppingPlan(retailer = 'mathem', revision = 1) {
  return {id: 'plan', revision, text: 'Risotto för 2', updated_at: '2026-10-29T12:00:00Z', retailer_id: retailer, warnings: [], budget_minor: 0, baskets: [
    {retailer_id: 'mathem', context: shops[0].context, status: 'ready', total: sek(2500), missing: 1},
    {retailer_id: 'willys', context: shops[1].context, status: 'ready', total: sek(2900), missing: 1},
  ], lines: [
    {id: 'salad', item: {label: 'Sallad', query: 'sallad', amount: {quantity: 200, unit: 'g'}, required_traits: []}, skipped: false, baskets: {mathem: notFound, willys: notFound}},
    {id: 'rice', item: {label: 'Ris', query: 'ris', amount: {quantity: 500, unit: 'g'}, required_traits: []}, skipped: false, baskets: {mathem: rice('Basmatiris', 2500), willys: rice('Jasminris', 2900)}},
  ]};
}
const summary = (plan) => ({id: plan.id, text: plan.text, retailer_id: plan.retailer_id, total: plan.baskets.find((b) => b.retailer_id === plan.retailer_id).total, updated_at: plan.updated_at});

async function app({retailers = shops, plans = [shoppingPlan()], search = '', setup = setupState(retailers), userAgent = 'Mozilla/5.0 Chrome/140'} = {}) {
  const elements = new Map([...html.matchAll(/<(\w+)[^>]*\sid="([^"]+)"[^>]*>/g)].map(([tag, name, id]) => { const node = new Element(name); node.hidden = /\shidden[\s>]/.test(tag); return [id, node]; }));
  elements.get('token').value = 'test-user';
  const documentEvents = new Map(), dispatched = [], timers = new Map(), requests = [], addresses = [], copied = [];
  let now = 0, nextID = 1, uuid = 0;
  const responses = {'/v1/setup': setup, '/v1/shopping-plans': {plans: plans.map(summary)}};
  for (const plan of plans) responses[`/v1/shopping-plans/${plan.id}`] = plan;
  const values = new Map();
  const storage = {getItem: (key) => values.get(key) || null, setItem: (key, value) => values.set(key, value), removeItem: (key) => values.delete(key)};
  const emit = (type, detail) => documentEvents.get(type)?.({detail: JSON.stringify(detail)});
  const context = createContext({
    document: {getElementById: (id) => elements.get(id), createElement: (tag) => new Element(tag), addEventListener: (type, callback) => documentEvents.set(type, callback), dispatchEvent: (event) => { dispatched.push(event.type); documentEvents.get(event.type)?.(event); }},
    navigator: {userAgent, clipboard: {writeText: async (text) => { copied.push(text); }}},
    window: {location: {search}, history: {replaceState: (state, title, url) => addresses.push(url)}, addEventListener() {}, scrollTo() {}},
    sessionStorage: storage, CustomEvent: class { constructor(type) { this.type = type; } }, URLSearchParams, Intl,
    crypto: {randomUUID: () => `key-${++uuid}`},
    fetch: async (path, options) => {
      const response = responses[path];
      assert.ok(response, `Unexpected request ${path}`);
      requests.push({path, ...options, body: options.body ? JSON.parse(options.body) : undefined});
      const [status, body] = typeof response === 'function' ? response(options) : [200, response];
      return {ok: status < 400, status, json: async () => body};
    },
    setTimeout: (callback, delay) => { const id = nextID++; timers.set(id, {callback, time: now + delay}); return id; },
    clearTimeout: (id) => timers.delete(id), setInterval: () => 1, clearInterval() {},
  });
  runInContext(source, context);
  await settle();
  return {
    elements, emit, requests, responses, dispatched, addresses, copied,
    run: (code) => runInContext(code, context),
    receipt: (state, created = new Date().toISOString(), lines = [{product_id: 'rice', status: 'unavailable', confirmed_quantity: 0, message: 'Not sold online at your Mathem store.'}]) => runInContext(`showFill(${JSON.stringify({id: 'transfer', state, retailer_id: 'mathem', basket_url: basketURL, created_at: created, receipt: {lines}})})`, context),
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
  for (let i = 0; i < 16; i++) await new Promise((resolve) => setImmediate(resolve));
}

const texts = (element) => element.children.map((node) => node.textContent.replaceAll('\u00a0', ' '));

test('the newest saved list opens on its own and cannot be edited', async () => {
  const ui = await app();
  assert.equal(ui.elements.get('title').textContent, 'Risotto för 2');
  assert.deepEqual(ui.addresses, ['/?plan=plan']);
  assert.match(ui.elements.get('missing').textContent, /1 item has no product and won’t be added/);
  assert.deepEqual(texts(ui.elements.get('lines')), ['Needs attention · 1', 'Sallad 200 g · No products found. Not found', 'Matched · 1', 'Ris 500 g · 1 × Basmatiris 25,00 kr']);
  assert.ok(ui.elements.get('lines').children.every((node) => node.listeners.click === undefined && node.children.every((child) => child.listeners.click === undefined)));
  assert.equal(ui.elements.get('start').hidden, true);
  assert.match(ui.elements.get('history').textContent, /Risotto för 2 · Mathem 25,00\skr/);
});

test('a list named in the address opens instead of the newest, and a missing one falls back', async () => {
  const older = {...shoppingPlan('willys'), id: 'older', text: 'Pannkakor'};
  let ui = await app({plans: [shoppingPlan(), older], search: '?plan=older'});
  assert.equal(ui.elements.get('title').textContent, 'Pannkakor');
  assert.equal(ui.elements.get('list-count').textContent, '2 items · Willys');
  ui = await app({plans: [shoppingPlan()], search: '?plan=gone'});
  assert.equal(ui.elements.get('title').textContent, 'Risotto för 2');
});

test('without lists the page is the setup, and a list made in ChatGPT replaces it', async () => {
  const ui = await app({plans: []});
  assert.equal(ui.elements.get('setup').hidden, false);
  assert.equal(ui.elements.get('intro').hidden, true);
  assert.equal(ui.elements.get('review').hidden, true);
  assert.equal(ui.elements.get('checkout').hidden, true);
  assert.equal(ui.elements.get('saved-lists').hidden, true);
  const made = shoppingPlan();
  ui.responses['/v1/shopping-plans'] = {plans: [summary(made)]};
  ui.responses['/v1/shopping-plans/plan'] = made;
  ui.emit('shop-it-extension-ready', {connected: true, client_id: 'browser'});
  await ui.run('follow()');
  await settle();
  assert.equal(ui.elements.get('title').textContent, 'Risotto för 2');
  assert.equal(ui.elements.get('intro').hidden, false);
  assert.equal(ui.elements.get('setup').hidden, true);
  assert.equal(ui.elements.get('start').hidden, true);
  assert.equal(ui.elements.get('saved-lists').hidden, false);
});

test('setup marks each step done and opens ChatGPT once Shop It is added there', async () => {
  const ui = await app({plans: []});
  const steps = () => ['step-extension', 'step-chatgpt', 'step-stores'].map((id) => ui.elements.get(id).className);
  assert.deepEqual(steps(), ['step', 'step', 'step']);
  assert.match(ui.elements.get('extension-install').textContent, /chrome:\/\/extensions .*Load unpacked \/Users\/me\/Library\/Application Support\/shop-it\/extension\/chromium/);
  find(ui.elements.get('extension-install'), (node) => node.className === 'copy' && node.textContent.endsWith('chromium')).listeners.click();
  await settle();
  assert.deepEqual(ui.copied, ['/Users/me/Library/Application Support/shop-it/extension/chromium']);
  assert.equal(ui.elements.get('install').href, 'codex://plugins/shop-it?marketplacePath=%2Fplugin');
  assert.equal(ui.elements.get('start').attributes['aria-disabled'], 'true');
  assert.equal(ui.elements.get('start').href, undefined);

  ui.emit('shop-it-extension-ready', {connected: true, client_id: 'browser'});
  assert.equal(ui.elements.get('extension-status').textContent, 'Connected');
  assert.equal(ui.elements.get('extension-install').hidden, true);

  const selected = setupState(shops); selected.browser_online = true; selected.stores[1].selected = true;
  const checked = structuredClone(selected); checked.stores[1].session = {retailer_id: 'willys', status: 'ready', label: 'Willys Fältöversten'};
  ui.responses['/v1/setup/stores'] = () => [200, selected];
  ui.responses['/v1/setup/stores/willys/check'] = () => [200, checked];
  find(ui.elements.get('stores'), (node) => node.tagName === 'button' && node.textContent === 'Willys').listeners.click();
  await settle();
  assert.deepEqual(ui.requests.find((request) => request.path === '/v1/setup/stores').body, {retailer_ids: ['willys']});
  assert.equal(ui.requests.at(-1).path, '/v1/setup/stores/willys/check');
  assert.match(ui.elements.get('stores').textContent, /Willys Fältöversten/);
  assert.deepEqual(steps(), ['step done', 'step', 'step done']);

  ui.responses['/v1/setup'] = {...checked, chatgpt_connected: true};
  await ui.run('follow()');
  await settle();
  assert.deepEqual(steps(), ['step done', 'step done', 'step done']);
  assert.equal(ui.elements.get('install').hidden, true);
  assert.equal(ui.elements.get('start').href, 'codex://new?path=%2Fplugin&prompt=Shop%20It');
  assert.equal(ui.elements.get('start').className, 'button primary');
});

test('a store that needs sign-in links to the store and is checked again on request', async () => {
  const state = setupState(shops, {browser_online: true});
  state.stores[0].selected = true;
  state.stores[0].session = {retailer_id: 'mathem', status: 'login_required'};
  const ui = await app({plans: [], setup: state});
  const link = find(ui.elements.get('stores'), (node) => node.tagName === 'a');
  assert.equal(link.textContent, 'Sign in ↗');
  assert.equal(link.href, basketURL);
  ui.responses['/v1/setup/stores/mathem/check'] = () => [409, {error: 'Connect the Shop It browser extension, then try again.'}];
  find(ui.elements.get('stores'), (node) => node.textContent === 'Check again').listeners.click();
  await settle();
  assert.match(ui.elements.get('message').textContent, /Connect the Shop It browser extension/);
  assert.equal(ui.elements.get('step-stores').className, 'step');
});

test('Firefox gets the temporary add-on steps', async () => {
  const ui = await app({plans: [], userAgent: 'Mozilla/5.0 Gecko/20100101 Firefox/144.0'});
  assert.match(ui.elements.get('extension-install').textContent, /about:debugging#\/runtime\/this-firefox .*Load Temporary Add-on… \/Users\/me\/Library\/Application Support\/shop-it\/extension\/firefox\/manifest\.json/);
});

test('the setup button reopens setup above a list', async () => {
  const ui = await app();
  ui.emit('shop-it-extension-ready', {connected: true, client_id: 'browser'});
  assert.equal(ui.elements.get('setup').hidden, true);
  assert.equal(ui.elements.get('setup-toggle').hidden, false);
  assert.equal(ui.elements.get('setup-toggle').className, 'attention');
  assert.equal(ui.elements.get('new-list').href, 'codex://new?path=%2Fplugin&prompt=Shop%20It');
  ui.elements.get('setup-toggle').listeners.click();
  assert.equal(ui.elements.get('setup').hidden, false);
  assert.equal(ui.elements.get('setup-title').hidden, true);
  assert.equal(ui.elements.get('setup-toggle').attributes['aria-expanded'], 'true');
  ui.elements.get('setup-toggle').listeners.click();
  assert.equal(ui.elements.get('setup').hidden, true);
});

test('the page follows edits made in ChatGPT', async () => {
  const ui = await app();
  const edited = shoppingPlan('mathem', 2);
  edited.updated_at = '2026-10-29T12:05:00Z';
  edited.lines[0].baskets.mathem = rice('Isbergssallad', 1990);
  ui.responses['/v1/shopping-plans'] = {plans: [summary(edited)]};
  ui.responses['/v1/shopping-plans/plan'] = edited;
  await ui.run('follow()');
  await settle();
  assert.match(ui.elements.get('lines').textContent, /Sallad 200 g · 1 × Isbergssallad/);
  assert.equal(ui.elements.get('missing').hidden, true);
});

test('choosing another shop switches the list, total and fill target', async () => {
  const ui = await app();
  ui.emit('shop-it-extension-ready', {connected: true, client_id: 'browser'});
  const cards = () => ui.elements.get('baskets').children;
  assert.match(cards()[1].textContent, /^Willys 29,00\skr 1 missing$/);
  assert.equal(ui.elements.get('transfer').textContent, 'Fill Mathem basket');
  ui.responses['/v1/shopping-plans/plan'] = () => [200, shoppingPlan('willys', 2)];
  cards()[1].listeners.click();
  await settle();
  const choice = ui.requests.find((request) => request.method === 'PATCH');
  assert.deepEqual(choice.body, {revision: 1, retailer_id: 'willys'});
  assert.deepEqual(cards().map((card) => card.attributes['aria-pressed']), ['false', 'true']);
  assert.match(ui.elements.get('total').textContent, /29,00/);
  assert.equal(ui.elements.get('transfer').textContent, 'Fill Willys basket');
});

test('filling waits for the extension and lets the app pick the browser', async () => {
  const ui = await app();
  assert.equal(ui.elements.get('setup').hidden, false);
  assert.equal(ui.elements.get('transfer').disabled, true);
  assert.equal(ui.elements.get('refresh').disabled, true);
  ui.emit('shop-it-extension-ready', {connected: false, error: 'Pairing failed.'});
  assert.match(ui.elements.get('extension-status').textContent, /Pairing failed/);
  assert.equal(ui.elements.get('extension-install').hidden, true);
  ui.emit('shop-it-extension-ready', {connected: true, client_id: 'browser'});
  assert.equal(ui.elements.get('setup').hidden, true);
  assert.equal(ui.elements.get('transfer').disabled, false);
  ui.responses['/v1/shopping-plans/plan/transfer'] = {id: 'transfer', state: 'pending', retailer_id: 'mathem', basket_url: basketURL, created_at: new Date().toISOString()};
  ui.responses['/v1/basket-transfers/transfer'] = {id: 'transfer', state: 'completed', retailer_id: 'mathem', basket_url: basketURL, receipt: {lines: [{product_id: 'rice', status: 'applied', confirmed_quantity: 1}]}};
  ui.elements.get('transfer').listeners.click();
  await settle();
  const request = ui.requests.find((request) => request.path === '/v1/shopping-plans/plan/transfer');
  assert.deepEqual(request.body, {revision: 1, acknowledge_warnings: false, allow_over_budget: false});
  assert.equal(request.headers['Idempotency-Key'], 'key-1');
  assert.ok(ui.dispatched.includes('shop-it-fill'));
  assert.match(ui.elements.get('receipt').textContent, /Opening Mathem in a new tab/);
  ui.advance(1000);
  await settle();
  assert.equal(ui.elements.get('filled').hidden, false);
  assert.equal(ui.elements.get('review-store').hidden, false);
  assert.equal(ui.elements.get('review-store').href, basketURL);
  assert.equal(ui.elements.get('transfer').textContent, 'Fill basket again');
  ui.elements.get('transfer').listeners.click();
  await settle();
  assert.equal(ui.requests.filter((request) => request.path === '/v1/shopping-plans/plan/transfer').at(-1).headers['Idempotency-Key'], 'key-2');
});

test('the store’s own explanation is shown when a fill is refused', async () => {
  const ui = await app();
  ui.emit('shop-it-extension-ready', {connected: true, client_id: 'browser'});
  ui.responses['/v1/shopping-plans/plan/transfer'] = () => [409, {error: 'Your Mathem store or delivery changed since these prices were checked. Refresh prices, then fill the basket.'}];
  ui.elements.get('transfer').listeners.click();
  await settle();
  assert.match(ui.elements.get('message').textContent, /Refresh prices, then fill the basket/);
  ui.responses['/v1/shopping-plans/plan/transfer'] = () => [409, {error: 'conflict'}];
  ui.elements.get('transfer').listeners.click();
  await settle();
  assert.match(ui.elements.get('message').textContent, /The list changed or still needs review/);
  ui.responses['/v1/shopping-plans/plan/refresh'] = {...shoppingPlan('mathem', 2), updated_at: '2026-10-29T13:00:00Z'};
  ui.elements.get('refresh').listeners.click();
  await settle();
  assert.deepEqual(ui.requests.find((request) => request.path === '/v1/shopping-plans/plan/refresh').body, {revision: 1});
  assert.equal(ui.elements.get('message').textContent, '');
});

test('unverified requirements and a blown budget must be accepted before filling', async () => {
  const plan = shoppingPlan();
  plan.lines[1].baskets.mathem = rice('Basmatiris', 2500, {unmet_traits: ['organic']});
  plan.budget_minor = 2000;
  plan.baskets[0].over_budget = true;
  const ui = await app({plans: [plan]});
  ui.emit('shop-it-extension-ready', {connected: true, client_id: 'browser'});
  assert.match(ui.elements.get('lines').textContent, /1 × Basmatiris · Not marked organic/);
  assert.equal(ui.elements.get('ack-wrap').hidden, false);
  assert.equal(ui.elements.get('over-wrap').hidden, false);
  assert.match(ui.elements.get('over-text').textContent, /Accept going 5,00\skr over your 20,00\skr budget/);
  assert.equal(ui.elements.get('transfer').disabled, true);
  ui.elements.get('ack').checked = true; ui.elements.get('ack').listeners.change();
  assert.equal(ui.elements.get('transfer').disabled, true);
  ui.elements.get('over').checked = true; ui.elements.get('over').listeners.change();
  assert.equal(ui.elements.get('transfer').disabled, false);
});

test('demo shops fill their basket without the extension', async () => {
  const plan = shoppingPlan();
  plan.baskets = [{...plan.baskets[0], context: demo[0].context}];
  const ui = await app({retailers: demo, plans: [plan]});
  assert.equal(ui.elements.get('setup').hidden, true);
  assert.equal(ui.elements.get('transfer').disabled, false);
  assert.equal(ui.elements.get('transfer').textContent, 'Fill Mathem demo basket');
  assert.match(ui.elements.get('estimate').textContent, /Demo prices/);
});

test('a partial fill marks the items that were not added', async () => {
  const ui = await app();
  ui.emit('shop-it-extension-ready', {connected: true, client_id: 'browser'});
  ui.receipt('partial');
  assert.match(ui.elements.get('receipt').textContent, /Some items were not added\. They are at the top of the list/);
  assert.match(ui.elements.get('lines').textContent, /Ris 500 g · 1 × Basmatiris · Not sold online at your Mathem store\. Unavailable/);
  assert.equal(ui.elements.get('transfer').textContent, 'Fill basket again');
  assert.equal(ui.elements.get('refresh').hidden, true);
});

test('a fill nobody picks up explains that the extension is not responding', async () => {
  const ui = await app();
  ui.emit('shop-it-extension-ready', {connected: true, client_id: 'browser'});
  ui.receipt('pending', new Date(Date.now() - 11000).toISOString());
  assert.match(ui.elements.get('receipt').textContent, /Waiting for the Shop It extension/);
  ui.receipt('expired');
  assert.match(ui.elements.get('receipt').textContent, /did not pick this up/);
});
