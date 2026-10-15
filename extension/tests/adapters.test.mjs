import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import '../assets/adapters.js';
import {applyTransfer, discoverSessions, localURL} from '../assets/runner.js';

const adapters = globalThis.ShipItAdapters;
const fixtures = JSON.parse(await readFile(new URL('./storefronts.json', import.meta.url)));
const context = {store_id: 'store-1', postal_code: '11122', fulfillment: 'delivery', memberships: [], session_id: 'session'};
function memoryJournal(initial = {}) {
  const records = structuredClone(initial);
  return {records, load: async (id) => structuredClone(records[id]), save: async (id, value) => { records[id] = structuredClone(value); }};
}
const input = {id: 'transfer-1', retailer_id: 'mathem', context, items: [{product_id: '2183', target_quantity: 2}]};

test('live response fixtures preserve identifiers, prices, packs and dietary evidence', () => {
  const axfood = adapters.axfoodProduct(fixtures.willys.results[0]);
  assert.equal(axfood.id, '101233933_ST');
  assert.deepEqual(axfood.pack, {quantity: 1500, unit: 'ml'});
  assert.equal(axfood.price.amount_minor, 1690);
  assert.equal(adapters.axfoodProduct(fixtures.hemkop.results[2]).traits.organic, 'yes');
  assert.equal(adapters.mathemProduct(fixtures.mathem.products[2]).traits.lactose_free, 'yes');
  const banana = adapters.axfoodProduct(fixtures.weighted[0]);
  assert.deepEqual(banana.pack, {quantity: 180, unit: 'g'});
  assert.equal(banana.price.amount_minor, 358);
  assert.equal(banana.price_kind, 'estimated');
  assert.equal(adapters.axfoodProduct({...fixtures.weighted[0], productBasketType: {code: 'KG'}}).pack.quantity, 1000);
  assert.equal(adapters.axfoodProduct({...fixtures.willys.results[0], displayVolume: 'unknown'}), null);
  assert.equal(adapters.axfoodProduct({...fixtures.willys.results[0], isDrugProduct: true}), null);
  assert.equal(adapters.axfoodProduct({...fixtures.willys.results[0], name: 'Organic milk'}).traits.organic, 'unknown');
  assert.deepEqual(adapters.pack('2 x 500 g'), {quantity: 1000, unit: 'g'});
  assert.deepEqual(adapters.pack('15 st'), {quantity: 15, unit: 'each'});
  assert.deepEqual(adapters.pack('1,5 l'), {quantity: 1500, unit: 'ml'});
});

test('Mathem pagination uses one-based page numbers and verifies session again', async () => {
  const paths = [];
  const adapter = adapters.create('mathem', {digest: async () => 'session', request: async (path) => {
    paths.push(path);
    if (path.includes('/user/session/')) return {id: 1};
    return {...fixtures.mathem, attributes: {total_hits: 100}};
  }});
  const session = await adapter.session();
  const result = await adapter.search({context: session.context, query: 'mjölk', cursor: '1'});
  assert.ok(paths.some((path) => path.includes('page=2')));
  assert.equal(result.next_cursor, '2');
  assert.equal(paths.filter((path) => path.includes('/user/session/')).length, 3);
  await assert.rejects(adapter.search({context, query: 'mjölk'}), /session changed/);
});

test('Mathem changes only the delta and leaves other products intact', async () => {
  const basket = {'2183': 5, '3681': 3};
  const calls = [];
  const adapter = adapters.create('mathem', {digest: async () => 'session', request: async (path, options) => {
    if (path.includes('/user/session/')) return {id: 1};
    if (options) {
      calls.push(options.body);
      for (const item of options.body.items) basket[item.productId] += item.quantity;
      return {};
    }
    return {items: Object.entries(basket).map(([id, quantity]) => ({product: {id: Number(id)}, quantity}))};
  }});
  const {context} = await adapter.session();
  await adapter.set(context, input.items[0], 5);
  assert.deepEqual(calls, [{items: [{productId: 2183, quantity: -3}]}]);
  assert.deepEqual(await adapter.read(context), {'2183': 2, '3681': 3});
  await assert.rejects(adapter.set(context, input.items[0], 5), /basket changed/);
});

test('Axfood sessions use the actively selected store before a time slot is booked', async () => {
  for (const id of ['willys', 'hemkop']) {
    const responses = {
      '/customer': {uid: 'anonymous', storeId: ''},
      '/cart': {deliveryModeCode: 'pickUpInStore', slotCode: '', postalCode: null, deliveryTimeSlot: {slotBaseStoreId: null, pickupStoreId: null}},
      '/store/active': {storeId: '2351', activelySelected: true},
    };
    const request = async (path) => responses[Object.keys(responses).find((suffix) => path.endsWith(suffix))];
    const session = await adapters.create(id, {digest: async () => 'session', request}).session();
    assert.equal(session.context.store_id, '2351');
    assert.equal(session.context.fulfillment, 'pickup');
    assert.equal(session.logged_in, false);
    responses['/store/active'] = {storeId: '2110', activelySelected: false};
    await assert.rejects(adapters.create(id, {digest: async () => 'session', request}).session(), /Select a store/);
  }
});

test('Axfood uses absolute quantities and the retailer basket unit', async () => {
  for (const id of ['willys', 'hemkop']) {
    const calls = [];
    const adapter = adapters.create(id, {digest: async () => 'session', request: async (path, options) => {
      if (path.endsWith('/customer')) return {uid: 'signed-in', storeId: 'store-1'};
      if (path.endsWith('/cart')) return {deliveryModeCode: 'homeDelivery', postalCode: '11122', products: []};
      if (path.endsWith('/store/active')) return {storeId: 'store-1', activelySelected: true};
      if (path.includes('/p/')) return {productBasketType: {code: 'ST'}};
      calls.push({path, body: options.body}); return {};
    }});
    const {context} = await adapter.session();
    await adapter.set(context, {product_id: '101233933_ST', target_quantity: 2}, 0);
    assert.equal(calls.length, 1);
    assert.equal(calls[0].path, id === 'willys' ? '/axfood/rest/v2/cart/addProducts' : '/axfood/rest/cart/addProducts');
    assert.deepEqual(calls[0].body, {storeId: 'store-1', products: [{productCodePost: '101233933_ST', qty: 2, pickUnit: 'pieces'}]});
  }
});

test('receipt replay never mutates the basket again', async () => {
  let quantity = 5, writes = 0;
  const journal = memoryJournal();
  const adapter = {read: async () => ({'2183': quantity}), set: async (_, item) => { writes++; quantity = item.target_quantity; }};
  const expected = {lines: [{product_id: '2183', status: 'applied', confirmed_quantity: 2}]};
  assert.deepEqual(await applyTransfer(input, adapter, journal, async () => {}), expected);
  assert.deepEqual(await applyTransfer(input, adapter, journal, async () => {}), expected);
  assert.equal(writes, 1);
});

test('restart after a mutation reads back without replaying a delta', async () => {
  const fingerprint = JSON.stringify({retailer_id: input.retailer_id, context: input.context, items: input.items});
  for (const [quantity, status] of [[2, 'applied'], [1, 'uncertain']]) {
    let writes = 0;
    const journal = memoryJournal({'transfer-1': {fingerprint, lines: {'2183': {phase: 'started', before: 0}}}});
    const result = await applyTransfer(input, {read: async () => ({'2183': quantity}), set: async () => { writes++; }}, journal, async () => {});
    assert.equal(result.lines[0].status, status);
    assert.equal(writes, 0);
  }
});

test('failed lease renewal prevents any basket mutation', async () => {
  let writes = 0;
  const result = await applyTransfer(input, {read: async () => ({}), set: async () => { writes++; }}, memoryJournal(), async () => { throw new Error('Lease lost.'); });
  assert.equal(writes, 0);
  assert.equal(result.lines[0].status, 'failed');
});

test('failed read-back produces uncertainty and failed login produces login_required', async () => {
  let reads = 0;
  const adapter = {read: async () => { if (++reads > 1) throw new Error('offline'); return {}; }, set: async () => {}};
  assert.equal((await applyTransfer(input, adapter, memoryJournal(), async () => {})).lines[0].status, 'uncertain');
  const login = {read: async () => { throw Object.assign(new Error('Sign in'), {status: 'login_required'}); }};
  assert.equal((await applyTransfer(input, login, memoryJournal(), async () => {})).lines[0].status, 'login_required');
});

test('connection settings reject redirects, remote hosts and embedded credentials', () => {
  assert.equal(localURL('http://127.0.0.1:18080'), 'http://127.0.0.1:18080');
  for (const url of ['https://example.com/', 'http://127.0.0.1.evil.test/', 'http://user:pass@127.0.0.1/', 'http://127.0.0.1/path', 'http://127.0.0.1/?token=x']) assert.throws(() => localURL(url));
});

test('session discovery tries every shop tab, preferring loaded and recently used ones', async () => {
  const origins = {willys: 'https://www.willys.se', hemkop: 'https://www.hemkop.se', mathem: 'https://www.mathem.se'};
  const tabs = [
    {id: 1, url: 'https://www.willys.se/', discarded: true, lastAccessed: 300},
    {id: 2, url: 'https://www.willys.se/', lastAccessed: 100},
    {id: 3, url: 'https://www.willys.se/erbjudanden', lastAccessed: 200},
    {id: 4, url: 'https://www.hemkop.se/', lastAccessed: 50},
    {id: 5, url: 'https://www.hemkop.se/handla', lastAccessed: 40},
  ];
  const calls = [];
  const unreachable = (id) => Object.assign(new Error(`Reload your ${id} tab to connect it.`), {unreachable: true});
  const {found, issues} = await discoverSessions(origins, tabs, async (tabID, id) => {
    calls.push(tabID);
    if (tabID === 3 || tabID === 1) throw unreachable(id);
    if (id === 'hemkop') throw new Error('Select a store and delivery or pickup on Hemköp.');
    return {retailer_id: id, name: id};
  });
  assert.deepEqual(calls, [3, 2, 4]);
  assert.deepEqual(found, [{retailer_id: 'willys', name: 'willys', tabID: 2}]);
  assert.deepEqual(issues, [{retailer_id: 'hemkop', message: 'Select a store and delivery or pickup on Hemköp.'}]);
  const stale = await discoverSessions(origins, [tabs[0]], async (tabID, id) => { throw unreachable(id); });
  assert.deepEqual(stale, {found: [], issues: [{retailer_id: 'willys', message: 'Reload your willys tab to connect it.'}]});
});
