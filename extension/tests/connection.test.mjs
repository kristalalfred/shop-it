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
    runtime: {id: 'ship-it', getURL: () => 'moz-extension://ship-it/', onMessage: {addListener: (callback) => { listener = callback; }}},
    storage: {local: {get: async () => ({settings})}},
    alarms: {create() {}, onAlarm: {addListener() {}}},
  };
  runInContext(background, createContext({browser: extension, ShipItAdapters: {origins: {}}, localURL, URL, setInterval() {}}));
  const sender = {id: 'ship-it', tab: {id: 1}, url: 'http://127.0.0.1:18080/'};
  const probe = (source = sender) => new Promise((resolve) => {
    if (!listener({type: 'connection'}, source, (result) => resolve(JSON.parse(JSON.stringify(result))))) resolve(null);
  });
  assert.deepEqual(await probe(), {connected: false});
  settings = {url: 'http://127.0.0.1:18080', token: 'private-client-token', client_id: 'browser'};
  assert.deepEqual(await probe(), {connected: true, client_id: 'browser'});
  assert.deepEqual(await probe({...sender, url: 'http://127.0.0.1:19090/'}), {connected: false});
  for (const url of ['https://example.com/', 'http://localhost:18080/', 'http://127.0.0.1:18080/other']) {
    assert.equal(await probe({...sender, url}), null);
  }
  assert.equal(await probe({...sender, id: 'other-extension'}), null);
});

test('page bridge pairs automatically and announces the paired client', async () => {
  const listeners = new Map(), events = [], sent = [];
  let connection = {connected: true, client_id: 'browser'}, pairing = {client_id: 'browser'};
  const context = createContext({
    browser: {runtime: {sendMessage: async (message) => {
      sent.push(message);
      if (message.type === 'connection') return connection;
      if (pairing instanceof Error) throw pairing;
      return pairing;
    }}},
    location: {hostname: '127.0.0.1', protocol: 'http:', pathname: '/', origin: 'http://127.0.0.1:18080'},
    document: {getElementById: () => ({value: 'user-token'}), addEventListener: (name, callback) => listeners.set(name, callback), dispatchEvent: (event) => events.push(event)},
    CustomEvent: class { constructor(type, {detail}) { this.type = type; this.detail = detail; } },
  });
  runInContext(bridge, context);
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(events[0].type, 'ship-it-extension-ready');
  assert.deepEqual(JSON.parse(events[0].detail), {connected: true, client_id: 'browser', error: ''});
  assert.deepEqual(sent.map((message) => message.type), ['connection']);
  connection = {connected: false};
  await listeners.get('ship-it-extension-probe')();
  assert.deepEqual(JSON.parse(JSON.stringify(sent.at(-1))), {type: 'pair', url: 'http://127.0.0.1:18080', token: 'user-token'});
  assert.deepEqual(JSON.parse(events.at(-1).detail), {connected: true, client_id: 'browser', error: ''});
  pairing = {error: 'Pairing failed.'};
  await listeners.get('ship-it-extension-probe')();
  assert.deepEqual(JSON.parse(events.at(-1).detail), {connected: false, client_id: '', error: 'Pairing failed.'});
  listeners.get('ship-it-fill')();
  assert.deepEqual(JSON.parse(JSON.stringify(sent.at(-1))), {type: 'fill'});
  pairing = new Error('Extension context invalidated.');
  await listeners.get('ship-it-extension-probe')();
  assert.deepEqual(JSON.parse(events.at(-1).detail), {connected: false, client_id: '', error: 'Reload the extension and this page.'});
});
