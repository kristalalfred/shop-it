import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {createContext, runInContext} from 'node:vm';
import {localURL} from '../assets/runner.js';

const background = (await readFile(new URL('../assets/background.js', import.meta.url), 'utf8')).replace(/^import .*;\n/gm, '');
const bridge = await readFile(new URL('../assets/bridge.js', import.meta.url), 'utf8');

test('connection probe reports only whether this local page has an enabled extension connection', async () => {
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
  settings = {url: 'http://127.0.0.1:18080', enabled: true, token: 'private-client-token', client_id: 'browser'};
  assert.deepEqual(await probe(), {connected: true});
  assert.deepEqual(await probe({...sender, url: 'http://127.0.0.1:19090/'}), {connected: false});
  settings.enabled = false;
  assert.deepEqual(await probe(), {connected: false});
  for (const url of ['https://example.com/', 'http://localhost:18080/', 'http://127.0.0.1:18080/other']) {
    assert.equal(await probe({...sender, url}), null);
  }
  assert.equal(await probe({...sender, id: 'other-extension'}), null);
});

test('page bridge announces pairing state on load and on subsequent probes', async () => {
  const listeners = new Map(), events = [];
  let connected = true;
  const context = createContext({
    browser: {runtime: {sendMessage: async (message) => { assert.equal(message.type, 'connection'); return {connected}; }}},
    location: {hostname: '127.0.0.1', protocol: 'http:', pathname: '/'},
    document: {addEventListener: (name, callback) => listeners.set(name, callback), dispatchEvent: (event) => events.push(event)},
    CustomEvent: class { constructor(type, {detail}) { this.type = type; this.detail = detail; } },
  });
  runInContext(bridge, context);
  await Promise.resolve();
  assert.equal(events[0].type, 'ship-it-extension-ready');
  assert.deepEqual(JSON.parse(events[0].detail), {connected: true});
  connected = false;
  await listeners.get('ship-it-extension-probe')();
  assert.deepEqual(JSON.parse(events.at(-1).detail), {connected: false});
});
