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

const fixtures = JSON.parse(await readFile(new URL('./storefronts.json', import.meta.url)));
const userToken = 'test-user';
const clientToken = 'test-browser';

test('real adapter protocol carries a reviewed plan through the backend and verifies its basket receipt', async (t) => {
  const dir = await mkdtemp(join(tmpdir(), 'ship-it-extension-test-'));
  t.after(() => rm(dir, {recursive: true, force: true}));
  const config = {listen: '127.0.0.1:0', api_token: userToken, adapter_timeout_ms: 5000, data_dir: join(dir, 'data'), retailers: [{id: 'willys', name: 'Willys', mode: 'browser', basket_url: 'https://www.willys.se/'}], clients: [{id: 'browser', token: clientToken}, {id: 'other', token: 'other-client'}]};
  const path = join(dir, 'config.json');
  await writeFile(path, JSON.stringify(config));
  const process = spawn(resolve('bin/ship-it'), ['-config', path], {stdio: ['ignore', 'pipe', 'pipe']});
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
  const basket = {'unrelated_ST': 7};
  let mutations = 0;
  const adapter = ShipItAdapters.create('willys', {digest: async () => 'session-1', request: async (path, options) => {
    if (path.endsWith('/customer')) return {uid: 'test-account', storeId: 'store-1'};
    if (path.endsWith('/cart')) return {postalCode: '11122', deliveryModeCode: 'homeDelivery', products: Object.entries(basket).map(([code, quantity]) => ({code, quantity, pickQuantity: 0, productBasketType: {code: 'ST'}}))};
    if (path.includes('/search?')) return fixtures.willys;
    if (path.includes('/p/')) return {productBasketType: {code: 'ST'}};
    if (path.endsWith('/cart/addProducts')) {
      mutations++;
      for (const item of options.body.products) basket[item.productCodePost] = item.qty;
      return {};
    }
    throw new Error(`Unexpected store request: ${path}`);
  }});
  const session = await adapter.session();
  await api('/v1/client/sessions', clientToken, 'POST', {sessions: [session]});
  const listed = await api('/v1/browser-sessions', userToken, 'GET');
  assert.equal(listed.sessions[0].client_id, 'browser');
  let polling = false;
  const poll = setInterval(async () => {
    if (polling) return;
    polling = true;
    try {
      const {jobs} = await api('/v1/client/search-claims', clientToken);
      for (const job of jobs) await api(`/v1/client/searches/${job.id}/result`, clientToken, 'PUT', {result: await adapter.search(job)});
    } finally { polling = false; }
  }, 20);
  t.after(() => clearInterval(poll));
  const plan = await api('/v1/shopping-plans', userToken, 'POST', {text: '2 litres milk', retailer_id: 'willys', context: session.context});
  clearInterval(poll);
  assert.equal(plan.status, 'ready');
  assert.equal(plan.lines[0].selected.target_quantity, 2);
  const transfer = await api(`/v1/shopping-plans/${plan.id}/transfer`, userToken, 'POST', {revision: plan.revision, client_id: 'browser', acknowledge_warnings: true}, 'end-to-end');
  const foreign = await api('/v1/client/claims', 'other-client');
  assert.equal(foreign, null);
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
  const replay = await api(`/v1/shopping-plans/${plan.id}/transfer`, userToken, 'POST', {revision: plan.revision, client_id: 'browser', acknowledge_warnings: true}, 'end-to-end');
  assert.equal(replay.id, transfer.id);
  const pairing = await api('/v1/clients/browser/pair', userToken);
  assert.equal(pairing.token, clientToken);
  assert.equal((await fetch(url + '/v1/clients/browser/pair', {method: 'POST'})).status, 401);
  assert.equal(await new Promise((resolve, reject) => { const req = get(url, {headers: {Host: 'evil.example'}}, (response) => { response.resume(); resolve(response.statusCode); }); req.on('error', reject); }), 403);
  const html = await (await fetch(url)).text();
  assert.ok(html.includes('value="test-user"'));
  assert.ok(html.includes('Connect browser extension'));
  assert.equal((await fetch(url + '/extension/firefox.zip')).status, 200);
  await api('/v1/client/sessions', clientToken, 'POST', {sessions: []});
  const offline = await fetch(url + '/v1/catalog/search', {method: 'POST', headers: {Authorization: `Bearer ${userToken}`, 'Content-Type': 'application/json'}, body: JSON.stringify({retailer_id: 'willys', context: session.context, query: 'milk'})});
  assert.equal(offline.status, 502);
  const stale = await fetch(url + `/v1/shopping-plans/${plan.id}/transfer`, {method: 'POST', headers: {Authorization: `Bearer ${userToken}`, 'Content-Type': 'application/json', 'Idempotency-Key': 'stale'}, body: JSON.stringify({revision: plan.revision, client_id: 'browser', acknowledge_warnings: true})});
  assert.equal(stale.status, 409);
});
