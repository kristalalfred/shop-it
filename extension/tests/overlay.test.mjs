import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {createContext, runInContext} from 'node:vm';

const source = await readFile(new URL('../assets/overlay.js', import.meta.url), 'utf8');

class Node {
  constructor(tag) { this.tag = tag; this.children = []; this.attributes = {}; this.listeners = {}; this.style = {}; this.className = ''; this.textContent = ''; this.parent = null; }
  addEventListener(name, callback) { this.listeners[name] = callback; }
  showModal() { assert.ok(this.parent, 'dialog opened before it was attached'); this.modal = true; }
  append(...nodes) { for (const node of nodes) { node.parent = this; this.children.push(node); } }
  remove() { if (this.parent) this.parent.children = this.parent.children.filter((node) => node !== this); this.parent = null; }
  setAttribute(name, value) { this.attributes[name] = value; }
  attachShadow({mode}) { assert.equal(mode, 'closed'); this.shadow = new Node('#shadow'); return this.shadow; }
  find(className) { return this.className.split(' ').includes(className) ? this : this.children.map((node) => node.find(className)).find(Boolean) || this.shadow?.find(className); }
}

function load() {
  const timers = new Map();
  let next = 1;
  const documentElement = new Node('html');
  const context = createContext({
    document: {documentElement, createElement: (tag) => new Node(tag)},
    setTimeout: (callback, delay) => { timers.set(next, {callback, delay}); return next++; },
    clearTimeout: (id) => timers.delete(id),
  });
  runInContext(source, context);
  return {overlay: context.ShopItOverlay, documentElement, timers};
}

test('the overlay shows progress on the shop page and is removed when done', () => {
  const {overlay, documentElement} = load();
  overlay.show({title: 'Filling your Willys basket', detail: 'Adding Röda Linser', progress: 0.25, count: 'Item 2 of 8'});
  assert.equal(documentElement.children.length, 1);
  const host = documentElement.children[0];
  assert.equal(host.tag, 'shop-it-overlay');
  const card = host.find('card');
  assert.equal(card.tag, 'dialog');
  assert.equal(card.modal, true);
  let prevented = false;
  card.listeners.cancel({preventDefault: () => { prevented = true; }});
  assert.equal(prevented, true);
  assert.equal(host.find('title').textContent, 'Filling your Willys basket');
  assert.equal(host.find('detail').textContent, 'Adding Röda Linser');
  assert.equal(host.find('fill').style.width, '25%');
  assert.equal(host.find('count').textContent, 'Item 2 of 8');
  overlay.show({title: 'Your basket is filled', detail: 'All 8 items are in your Willys basket.', progress: 1, count: '8 of 8 added', finished: true});
  assert.equal(documentElement.children.length, 1);
  assert.equal(host.find('fill').style.width, '100%');
  assert.equal(host.find('card').className, 'card finished');
  overlay.remove();
  assert.equal(documentElement.children.length, 0);
});

test('an abandoned overlay removes itself after a minute without updates', () => {
  const {overlay, documentElement, timers} = load();
  overlay.show({title: 'Filling your Mathem basket', progress: 2});
  assert.equal(documentElement.children[0].find('fill').style.width, '100%');
  overlay.show({title: 'Filling your Mathem basket', progress: 0.5});
  assert.equal(timers.size, 1);
  const [{callback, delay}] = timers.values();
  assert.equal(delay, 60000);
  callback();
  assert.equal(documentElement.children.length, 0);
});
