import test from 'node:test';
import assert from 'node:assert/strict';
import '../assets/adapters.js';
import {applyTransfer, localURL} from '../assets/runner.js';

const adapters = globalThis.ShipItAdapters;
const context = {store_id: 'mathem', postal_code: '00000', fulfillment: ''};
function memoryJournal(initial = {}) {
  const records = structuredClone(initial);
  return {records, load: async (id) => structuredClone(records[id]), save: async (id, value) => { records[id] = structuredClone(value); }};
}
const input = {id: 'transfer-1', retailer_id: 'mathem', context, items: [{product_id: '2183', target_quantity: 2}]};

test('Mathem changes only the delta and leaves other products intact', async () => {
  const basket = {'2183': 5, '3681': 3};
  const calls = [];
  const adapter = adapters.create('mathem', {request: async (path, options) => {
    if (options) {
      calls.push(options.body);
      for (const item of options.body.items) basket[item.productId] += item.quantity;
      return {};
    }
    return {items: Object.entries(basket).map(([id, quantity]) => ({product: {id: Number(id)}, quantity}))};
  }});
  await adapter.set(input.items[0], 5);
  assert.deepEqual(calls, [{items: [{productId: 2183, quantity: -3}]}]);
  assert.deepEqual(await adapter.read(), {'2183': 2, '3681': 3});
  await assert.rejects(adapter.set(input.items[0], 5), /basket changed/);
});

test('Axfood uses absolute quantities and the basket unit of the shopper session', async () => {
  for (const id of ['willys', 'hemkop']) {
    const calls = [];
    const adapter = adapters.create(id, {request: async (path, options) => {
      if (path.endsWith('/cart')) return {products: []};
      if (path.includes('/p/')) return {productBasketType: {code: 'KG'}};
      calls.push({path, body: options.body}); return {};
    }});
    await adapter.set({product_id: '100254920_KG', target_quantity: 2}, 0);
    assert.equal(calls.length, 1);
    assert.equal(calls[0].path, id === 'willys' ? '/axfood/rest/v2/cart/addProducts' : '/axfood/rest/cart/addProducts');
    assert.deepEqual(calls[0].body, {products: [{productCodePost: '100254920_KG', qty: 2, pickUnit: 'kilogram'}]});
  }
});

test('Axfood baskets with frozen products stay editable while placed orders do not', async () => {
  const cart = {frozen: true, orderReference: null, notEditableAfterOrder: false, products: [{code: '101349480_ST', pickQuantity: 1, quantity: 1}]};
  const adapter = adapters.create('willys', {request: async () => cart});
  assert.deepEqual(await adapter.read(), {'101349480_ST': 1});
  cart.orderReference = '1234567';
  await assert.rejects(adapter.read(), /existing order/);
});

test('products the shopper store does not sell are reported as unavailable', async () => {
  const failure = (httpStatus, body) => Object.assign(new Error(`Store request failed (${httpStatus}).`), {status: 'failed', httpStatus, body});
  const missing = adapters.create('hemkop', {request: async (path) => {
    if (path.endsWith('/cart')) return {products: []};
    throw failure(404, '');
  }});
  await assert.rejects(missing.set({product_id: '101543074_ST', target_quantity: 1}, 0), (error) => error.status === 'unavailable' && /Not sold online at your Hemköp store/.test(error.message));
  const rejected = adapters.create('willys', {request: async (path, options) => {
    if (path.endsWith('/cart')) return {products: []};
    if (path.includes('/p/')) return {productBasketType: {code: 'ST'}};
    if (options) throw failure(400, 'product.not.saleable.online');
  }});
  await assert.rejects(rejected.set({product_id: '101543074_ST', target_quantity: 1}, 0), (error) => error.status === 'unavailable');
  const journal = memoryJournal();
  const receipt = await applyTransfer({...input, retailer_id: 'willys', items: [{product_id: '101543074_ST', target_quantity: 1}]}, {read: async () => ({}), set: () => rejected.set({product_id: '101543074_ST', target_quantity: 1}, 0)}, journal, async () => {});
  assert.deepEqual(receipt.lines, [{product_id: '101543074_ST', status: 'unavailable', confirmed_quantity: 0, message: 'Not sold online at your Willys store.'}]);
});

test('the transfer reports progress before each item', async () => {
  const seen = [];
  const items = [{product_id: '2183', target_quantity: 2}, {product_id: '3681', target_quantity: 1}];
  const adapter = {read: async () => Object.fromEntries(items.map((item) => [item.product_id, item.target_quantity]))};
  await applyTransfer({...input, items}, adapter, memoryJournal(), async () => {}, async (index) => { seen.push(index); });
  assert.deepEqual(seen, [0, 1]);
});

test('receipt replay never mutates the basket again', async () => {
  let quantity = 5, writes = 0;
  const journal = memoryJournal();
  const adapter = {read: async () => ({'2183': quantity}), set: async (item) => { writes++; quantity = item.target_quantity; }};
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

test('a shop tab that never loads fails every line without touching the basket', async () => {
  const closed = new Error('Mathem did not finish loading. Try again.');
  const result = await applyTransfer({...input, items: [...input.items, {product_id: '3681', target_quantity: 1}]}, {read: async () => { throw closed; }, set: async () => { throw new Error('unexpected write'); }}, memoryJournal(), async () => {});
  assert.deepEqual(result.lines.map((line) => [line.status, line.message]), [['failed', closed.message], ['failed', closed.message]]);
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

test('session checks require an observed store and searches reject changes to the delivery context', async () => {
  let storeID = 'store-1';
  const calls = [];
  const adapter = ShipItAdapters.create('willys', {request: async (path) => {
    calls.push(path);
    if (path.endsWith('/store/active')) return {storeId: storeID, name: 'Willys Stockholm', activelySelected: true};
    if (path.endsWith('/cart')) return {products: [], deliveryAddress: {postalCode: '111 22'}, deliveryModeCode: 'homeDelivery'};
    return {results: [], pagination: {numberOfPages: 1}};
  }});
  const session = await adapter.session();
  assert.equal(session.status, 'ready');
  assert.deepEqual(session.context, {store_id: 'store-1', postal_code: '11122', fulfillment: 'delivery'});
  await adapter.search({query: 'mjölk', page: 0, context: session.context});
  assert.ok(calls.some((path) => path.startsWith('/axfood/rest/v2/search?')));
  storeID = 'different-store';
  await assert.rejects(adapter.search({query: 'mjölk', page: 0, context: session.context}), /settings changed/);
  const unconfigured = ShipItAdapters.create('willys', {request: async () => ({products: []})});
  assert.equal((await unconfigured.session()).status, 'setup_required');
  const signedOut = ShipItAdapters.create('mathem', {request: async () => { throw Object.assign(new Error('Sign in'), {status: 'login_required'}); }});
  assert.equal((await signedOut.session()).status, 'login_required');
});

test('a store change while reading search results is never accepted as a price observation', async () => {
  let selected = 'store-1';
  const adapter = ShipItAdapters.create('willys', {request: async (path) => {
    if (path.endsWith('/store/active')) return {storeId: selected, activelySelected: true};
    if (path.endsWith('/cart')) return {deliveryAddress: {postalCode: '11122'}, deliveryModeCode: 'homeDelivery'};
    selected = 'store-2';
    return {results: []};
  }});
  const {context} = await adapter.session();
  await assert.rejects(adapter.search({query: 'milk', page: 0, context}), /settings changed during search/);
});

test('Mathem uses the selected delivery address and exposes only its postcode and identifiers', async () => {
  const paths = [];
  const adapter = ShipItAdapters.create('mathem', {request: async (path) => {
    paths.push(path);
    if (path.includes('/slot-picker/slots/')) return {cartInfo: {deliveryAddress: {id: 42}, deliverySlot: {id: 123}}};
    if (path.includes('/user/addresses/address/')) return {zipCode: '111 22', addressDisplay: 'Private address', additionalInfo: {doorCode: 'private'}};
    throw new Error(`Unexpected read ${path}`);
  }});
  const session = await adapter.session();
  assert.equal(session.status, 'ready');
  assert.deepEqual(session.context, {session_id: 'address:42', store_id: 'mathem', postal_code: '11122', fulfillment: 'delivery', delivery_slot_id: '123'});
  assert.equal(JSON.stringify(session).includes('Private address'), false);
  assert.equal(paths.length, 2);
});

test('default Axfood stores do not count as a user connection', async () => {
  const adapter = ShipItAdapters.create('hemkop', {request: async (path) => path.endsWith('/store/active') ? {storeId: '4003', activelySelected: false, address: {postalCode: '74634'}} : {deliveryModeCode: 'pickupInStore'}});
  assert.equal((await adapter.session()).status, 'setup_required');
});
