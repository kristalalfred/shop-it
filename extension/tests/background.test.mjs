import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {createContext, runInContext} from 'node:vm';
import '../assets/adapters.js';
import {api, applyTransfer, localURL} from '../assets/runner.js';

const source = (await readFile(new URL('../assets/background.js', import.meta.url), 'utf8')).replace(/^import .*;\n/gm, '');
const settings = {url: 'http://127.0.0.1:24816', token: 'client-token', client_id: 'browser'};

function load(store, tabs) {
  const extension = {
    runtime: {id: 'shop-it', getURL: () => 'moz-extension://shop-it/', onMessage: {addListener() {}}},
    storage: {local: {get: async (key) => ({[key]: store[key]}), set: async (values) => { Object.assign(store, values); }, remove: async (key) => { delete store[key]; }}},
    alarms: {create() {}, onAlarm: {addListener() {}}},
    tabs,
  };
  runInContext(source, createContext({browser: extension, ShopItAdapters, api, applyTransfer, localURL, URL, setTimeout, setInterval() {}}));
}

async function until(condition) {
  const deadline = Date.now() + 5000;
  while (!condition()) {
    if (Date.now() > deadline) throw new Error('Timed out.');
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
}

test('concurrent commands for a shop without an open tab share one new tab', async () => {
  const store = {settings}, tabs = [], results = [];
  let commands = Array.from({length: 6}, (_, index) => ({id: `command-${index}`, retailer_id: 'willys', operation: 'search', input: {}}));
  globalThis.fetch = async (url, options) => {
    const {pathname} = new URL(url);
    if (pathname === '/v1/client/commands') {
      const batch = commands;
      commands = [];
      return Response.json({commands: batch});
    }
    if (pathname === '/v1/client/claims') return new Response(null, {status: 204});
    results.push(JSON.parse(options.body));
    if (results.length === 6) delete store.settings;
    return Response.json({ok: true});
  };
  load(store, {
    query: async () => tabs.filter((tab) => tab.loaded),
    create: async ({url}) => {
      const tab = {id: tabs.length + 1, url, loaded: false};
      tabs.push(tab);
      setTimeout(() => { tab.loaded = true; }, 50);
      return tab;
    },
    sendMessage: async (id, message) => {
      if (!tabs.find((tab) => tab.id === id)?.loaded) throw new Error('No content script.');
      return {result: message.operation === 'ready' || {products: []}};
    },
  });
  await until(() => results.length === 6);
  assert.equal(tabs.length, 1);
  assert.deepEqual(results, Array(6).fill({result: {products: []}}));
});
