import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {createContext, runInContext} from 'node:vm';
import {localURL} from '../assets/runner.js';

const background = (await readFile(new URL('../assets/background.js', import.meta.url), 'utf8')).replace(/^import .*;\n/gm, '');
const bridge = await readFile(new URL('../assets/bridge.js', import.meta.url), 'utf8');

test('connection probe reports only whether this local page is paired and with which client', async () => {
  let listener, settings;
  const extension = {
    runtime: {id: 'shop-it', getURL: () => 'moz-extension://shop-it/', onMessage: {addListener: (callback) => { listener = callback; }}},
    storage: {local: {get: async () => ({settings})}},
    alarms: {create() {}, onAlarm: {addListener() {}}},
  };
  runInContext(background, createContext({browser: extension, ShopItAdapters: {origins: {}}, localURL, URL}));
  const sender = {id: 'shop-it', tab: {id: 1}, url: 'http://127.0.0.1:24816/'};
  const probe = (source = sender) => new Promise((resolve) => {
    if (!listener({type: 'connection'}, source, (result) => resolve(JSON.parse(JSON.stringify(result))))) resolve(null);
  });
  assert.deepEqual(await probe(), {connected: false});
  settings = {url: 'http://127.0.0.1:24816', token: 'private-client-token', client_id: 'browser'};
  assert.deepEqual(await probe(), {connected: true, client_id: 'browser'});
  assert.deepEqual(await probe({...sender, url: 'http://127.0.0.1:19090/'}), {connected: false});
  for (const url of ['https://example.com/', 'http://localhost:24816/', 'http://127.0.0.1:24816/other']) {
    assert.equal(await probe({...sender, url}), null);
  }
  assert.equal(await probe({...sender, id: 'other-extension'}), null);
});

test('page bridge pairs on every probe and announces the paired client', async () => {
  const listeners = new Map(), events = [], sent = [];
  let token = 'user-token', pairing = {client_id: 'browser'};
  const context = createContext({
    browser: {runtime: {sendMessage: async (message) => {
      sent.push(message);
      if (message.type === 'connection') return {connected: true, client_id: 'browser'};
      if (pairing instanceof Error) throw pairing;
      return pairing;
    }}},
    location: {hostname: '127.0.0.1', protocol: 'http:', pathname: '/', origin: 'http://127.0.0.1:24816'},
    document: {getElementById: () => ({value: token}), addEventListener: (name, callback) => listeners.set(name, callback), dispatchEvent: (event) => events.push(event)},
    CustomEvent: class { constructor(type, {detail}) { this.type = type; this.detail = detail; } },
  });
  runInContext(bridge, context);
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(events[0].type, 'shop-it-extension-ready');
  assert.deepEqual(JSON.parse(events[0].detail), {connected: true, client_id: 'browser', error: ''});
  assert.deepEqual(JSON.parse(JSON.stringify(sent)), [{type: 'pair', url: 'http://127.0.0.1:24816', token: 'user-token'}]);
  await listeners.get('shop-it-extension-probe')();
  assert.deepEqual(sent.map((message) => message.type), ['pair', 'pair']);
  pairing = {error: 'Pairing failed.'};
  await listeners.get('shop-it-extension-probe')();
  assert.deepEqual(JSON.parse(events.at(-1).detail), {connected: false, client_id: '', error: 'Pairing failed.'});
  token = '';
  await listeners.get('shop-it-extension-probe')();
  assert.equal(sent.at(-1).type, 'connection');
  assert.deepEqual(JSON.parse(events.at(-1).detail), {connected: true, client_id: 'browser', error: ''});
  listeners.get('shop-it-fill')();
  assert.deepEqual(JSON.parse(JSON.stringify(sent.at(-1))), {type: 'fill'});
  token = 'user-token';
  pairing = new Error('Extension context invalidated.');
  await listeners.get('shop-it-extension-probe')();
  assert.deepEqual(JSON.parse(events.at(-1).detail), {connected: false, client_id: '', error: 'Reload the extension and this page.'});
});
