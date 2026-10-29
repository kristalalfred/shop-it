import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtemp, readFile, writeFile, rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join, resolve} from 'node:path';
import {spawn} from 'node:child_process';
import {once} from 'node:events';
import {get} from 'node:http';
import '../assets/adapters.js';
import {applyTransfer} from '../assets/runner.js';

const fixtures = JSON.parse(await readFile(new URL('../../internal/retailer/testdata/storefronts.json', import.meta.url)));
const userToken = 'test-user';
const clientToken = 'test-browser';

test('ChatGPT plans through the extension and the page fills the basket with the real adapter', async (t) => {
  const dir = await mkdtemp(join(tmpdir(), 'shop-it-extension-test-'));
  t.after(() => rm(dir, {recursive: true, force: true}));
  const config = {listen: '127.0.0.1:0', api_token: userToken, adapter_timeout_ms: 5000, data_dir: join(dir, 'data'), retailers: [{id: 'willys', name: 'Willys', mode: 'browser', basket_url: 'https://www.willys.se/varukorg'}], clients: [{id: 'browser', token: clientToken}, {id: 'other', token: 'other-client'}]};
  const path = join(dir, 'config.json');
  await writeFile(path, JSON.stringify(config));
  const process = spawn(resolve('bin/shop-it'), ['-config', path], {stdio: ['ignore', 'pipe', 'pipe']});
  t.after(async () => {
    if (process.exitCode === null) { process.kill('SIGINT'); await once(process, 'exit'); }
  });
  const url = await new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('Backend did not become ready.')), 5000);
    let output = '';
    process.stdout.on('data', (data) => {
      output += data;
      const match = output.match(/listening on (http:\/\/[^\s]+)/);
      if (match) { clearTimeout(timer); resolve(match[1]); }
    });
    process.on('error', (error) => { clearTimeout(timer); reject(error); });
    process.on('exit', (code) => { clearTimeout(timer); reject(new Error(`Backend exited ${code}.`)); });
  });
  async function api(path, token, method = 'POST', body, key) {
    const response = await fetch(url + path, {method, headers: {Authorization: `Bearer ${token}`, ...(body ? {'Content-Type': 'application/json'} : {}), ...(key ? {'Idempotency-Key': key} : {})}, ...(body ? {body: JSON.stringify(body)} : {})});
    if (response.status === 204) return null;
    const result = await response.json();
    assert.ok(response.ok, `${path}: ${response.status} ${JSON.stringify(result)}`);
    return result;
  }
  let call = 0;
  async function chatgpt(name, args) {
    const response = await fetch(url + '/mcp', {method: 'POST', headers: {Authorization: `Bearer ${userToken}`, 'Content-Type': 'application/json', Accept: 'application/json, text/event-stream'}, body: JSON.stringify({jsonrpc: '2.0', id: ++call, method: 'tools/call', params: {name, arguments: args}})});
    const {result, error} = await response.json();
    assert.ok(result && !result.isError, `${name}: ${JSON.stringify(error || result)}`);
    return result.structuredContent;
  }
  const basket = {'unrelated_ST': 7}, searches = [];
  let mutations = 0;
  const adapter = ShopItAdapters.create('willys', {request: async (path, options) => {
    if (path.endsWith('/store/active')) return {storeId: '2110', activelySelected: true, name: 'Willys Stockholm'};
    if (path.endsWith('/cart')) return {deliveryModeCode: 'homeDelivery', deliveryAddress: {postalCode: '111 22'}, products: Object.entries(basket).map(([code, quantity]) => ({code, quantity, pickQuantity: 0, productBasketType: {code: 'ST'}}))};
    if (path.includes('/search?')) { searches.push(path); return fixtures.willys; }
    if (path.includes('/p/')) return {productBasketType: {code: 'ST'}};
    if (path.endsWith('/cart/addProducts')) {
      mutations++;
      for (const item of options.body.products) basket[item.productCodePost] = item.qty;
      return {};
    }
    throw new Error(`Unexpected store request: ${path}`);
  }});
  let browsing = true;
  const browser = (async () => {
    while (browsing) {
      const {commands} = await api('/v1/client/commands?wait=1', clientToken);
      for (const command of commands) {
        let reply;
        try { reply = {result: await adapter[command.operation](command.input)}; } catch (error) { reply = {error: error.message, status: 'failed'}; }
        await api(`/v1/client/commands/${command.id}/result`, clientToken, 'PUT', reply);
      }
    }
  })().catch((error) => error);
  await chatgpt('configure_shop_it', {retailer_ids: ['willys']});
  const connected = await chatgpt('connect_store', {retailer_id: 'willys'});
  assert.deepEqual(connected.stores[0].session.context, {store_id: '2110', postal_code: '11122', fulfillment: 'delivery'});
  const created = await chatgpt('create_shopping_plan', {text: '2 litres milk', items: [{label: 'Milk', query: 'mjölk', amount: {quantity: 2000, unit: 'ml'}, required_traits: []}]});
  assert.equal(searches[0], '/axfood/rest/v2/search?q=mj%C3%B6lk&page=0&size=40');
  assert.equal(created.review_url, `http://127.0.0.1:0/?plan=${created.plan.id}`);
  const line = created.plan.lines[0];
  const chosen = await chatgpt('select_products', {plan_id: created.plan.id, revision: created.plan.revision, selections: [{line_id: line.id, retailer_id: 'willys', product_id: '101476218_ST'}]});
  const plan = chosen.plan;
  assert.equal(plan.baskets[0].status, 'ready');
  assert.equal(plan.lines[0].baskets.willys.selected.target_quantity, 2);
  const transfer = await api(`/v1/shopping-plans/${plan.id}/transfer`, userToken, 'POST', {revision: plan.revision}, 'end-to-end');
  assert.deepEqual(transfer.context, {store_id: '2110', postal_code: '11122', fulfillment: 'delivery'});
  assert.equal(await api('/v1/client/claims', 'other-client'), null);
  const claimed = await api('/v1/client/claims', clientToken);
  assert.equal(claimed.id, transfer.id);
  const records = {};
  const journal = {load: async (id) => records[id], save: async (id, record) => { records[id] = record; }};
  const receipt = await applyTransfer(claimed, adapter, journal, () => api(`/v1/client/transfers/${claimed.id}/renew`, clientToken));
  await api(`/v1/client/transfers/${claimed.id}/result`, clientToken, 'PUT', receipt);
  assert.equal((await api(`/v1/basket-transfers/${claimed.id}`, userToken, 'GET')).state, 'completed');
  assert.equal(basket[claimed.items[0].product_id], 2);
  assert.equal(basket.unrelated_ST, 7);
  await applyTransfer(claimed, adapter, journal, async () => {});
  assert.equal(mutations, 1);
  const replay = await api(`/v1/shopping-plans/${plan.id}/transfer`, userToken, 'POST', {revision: plan.revision}, 'end-to-end');
  assert.equal(replay.id, transfer.id);
  const search = await api('/v1/catalog/search', userToken, 'POST', {retailer_id: 'willys', context: transfer.context, query: 'mjölk'});
  assert.ok(search.products.length > 0);
  assert.equal(searches.length, 2);
  const elsewhere = await fetch(url + '/v1/catalog/search', {method: 'POST', headers: {Authorization: `Bearer ${userToken}`, 'Content-Type': 'application/json'}, body: JSON.stringify({retailer_id: 'willys', context: {store_id: 'willys', postal_code: '00000', fulfillment: ''}, query: 'milk'})});
  assert.equal(elsewhere.status, 409);
  const pairing = await api('/v1/clients/browser/pair', userToken);
  assert.equal(pairing.token, clientToken);
  assert.equal((await fetch(url + '/v1/clients/browser/pair', {method: 'POST'})).status, 401);
  assert.equal(await new Promise((resolve, reject) => { const req = get(url, {headers: {Host: 'evil.example'}}, (response) => { response.resume(); resolve(response.statusCode); }); req.on('error', reject); }), 403);
  const html = await (await fetch(url)).text();
  assert.ok(html.includes('value="test-user"'));
  assert.ok(!html.includes('<textarea') && !html.includes('<form'));
  assert.equal((await fetch(url + '/extension/firefox.zip')).status, 200);
  for (const path of ['/plugin', '/v1/interpretations', '/v1/plugin/tools/open_shop_it']) {
    assert.equal((await fetch(url + path, {method: path === '/plugin' ? 'GET' : 'POST', headers: {Authorization: `Bearer ${userToken}`}})).status, 404);
  }
  assert.equal((await fetch(url + '/v1/shopping-plans', {method: 'POST', headers: {Authorization: `Bearer ${userToken}`, 'Content-Type': 'application/json'}, body: '{"text":"milk"}'})).status, 405);
  browsing = false;
  assert.equal(await browser, undefined);
});
