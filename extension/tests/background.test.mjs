import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {createContext, runInContext} from 'node:vm';
import '../assets/adapters.js';
import {api, applyTransfer, localURL} from '../assets/runner.js';

const source = (await readFile(new URL('../assets/background.js', import.meta.url), 'utf8')).replace(/^import .*;\n/gm, '');
const settings = {url: 'http://127.0.0.1:24816', token: 'client-token', client_id: 'browser'};

function load(store, tabs = {}) {
  let listener;
  const extension = {
    runtime: {id: 'shop-it', getURL: () => 'moz-extension://shop-it/', onMessage: {addListener: (callback) => { listener = callback; }}},
    storage: {local: {get: async (key) => ({[key]: store[key]}), set: async (values) => { Object.assign(store, values); }, remove: async (key) => { delete store[key]; }}},
    alarms: {create() {}, onAlarm: {addListener() {}}},
    tabs,
  };
  runInContext(source, createContext({browser: extension, ShopItAdapters, api, applyTransfer, localURL, URL, setTimeout, setInterval, clearInterval}));
  return listener;
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

test('pairing from the Shop It page replaces a stale client token', async () => {
  const store = {settings: {...settings, token: 'stale-token'}};
  globalThis.fetch = async (url, options) => {
    const {pathname} = new URL(url);
    if (pathname === '/v1/clients') return Response.json({clients: [{id: 'browser'}]});
    if (pathname === '/v1/clients/browser/pair') return Response.json({id: 'browser', token: 'fresh-token'});
    if (options.headers.Authorization !== 'Bearer fresh-token') return Response.json({error: 'unauthorized'}, {status: 401});
    if (pathname === '/v1/client/commands') return Response.json({commands: []});
    return new Response(null, {status: 204});
  };
  const listener = load(store);
  const sender = {id: 'shop-it', tab: {id: 1}, url: 'http://127.0.0.1:24816/'};
  const reply = await new Promise((resolve) => listener({type: 'pair', url: 'http://127.0.0.1:24816', token: 'user-token'}, sender, resolve));
  assert.deepEqual(JSON.parse(JSON.stringify(reply)), {client_id: 'browser'});
  assert.equal(store.settings.token, 'fresh-token');
  delete store.settings;
});

test('the extension waits for work at the app and never asks more than once a second', async () => {
  const store = {settings}, polls = [];
  globalThis.fetch = async (url) => {
    const {pathname, search} = new URL(url);
    if (pathname === '/v1/client/commands') {
      polls.push({search, at: Date.now()});
      if (polls.length === 2) delete store.settings;
      return Response.json({commands: []});
    }
    return new Response(null, {status: 204});
  };
  load(store);
  await until(() => polls.length === 2);
  assert.deepEqual(polls.map((poll) => poll.search), ['?wait=20', '?wait=20']);
  assert.ok(polls[1].at - polls[0].at >= 990, `polled again after ${polls[1].at - polls[0].at} ms`);
});

test('an unreachable app is retried every second until it comes back', async () => {
  const store = {settings}, attempts = [];
  globalThis.fetch = async (url) => {
    attempts.push(Date.now());
    if (attempts.length <= 2) throw new TypeError('Failed to fetch');
    delete store.settings;
    return new URL(url).pathname === '/v1/client/commands' ? Response.json({commands: []}) : new Response(null, {status: 204});
  };
  load(store);
  await until(() => attempts.length >= 3);
  assert.ok(attempts[1] - attempts[0] >= 990, `retried after ${attempts[1] - attempts[0]} ms`);
});
