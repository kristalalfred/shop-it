import {api, applyTransfer, localURL} from './runner.js';
import './adapters.js';

const extension = globalThis.browser || globalThis.chrome;
let polling = false;
let connection = 'Open Shop It to connect.';
let basket = '';
const storage = extension.storage.local;
if (storage.setAccessLevel) storage.setAccessLevel({accessLevel: 'TRUSTED_CONTEXTS'});

async function storeCall(tabID, retailerID, operation, body = {}) {
  const reply = await extension.tabs.sendMessage(tabID, {type: 'store-operation', retailer_id: retailerID, operation, ...body});
  if (!reply) throw new Error('The shop tab did not respond.');
  if (reply.error) throw Object.assign(new Error(reply.error), {status: reply.status});
  return reply.result;
}

async function openShop(input) {
  const name = ShopItAdapters.names[input.retailer_id];
  const url = new URL(input.basket_url);
  if (!name || url.origin !== ShopItAdapters.origins[input.retailer_id]) throw new Error('Unsupported shop.');
  const tab = await extension.tabs.create({url: url.href, active: true});
  const deadline = Date.now() + 30000;
  while (Date.now() < deadline) {
    try {
      await storeCall(tab.id, input.retailer_id, 'ready');
      return tab.id;
    } catch {}
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  throw new Error(`${name} did not finish loading. Try again.`);
}

async function findShopTab(retailerID, origin) {
  for (const tab of await extension.tabs.query({url: `${origin}/*`})) {
    try {
      await storeCall(tab.id, retailerID, 'ready');
      return tab.id;
    } catch {}
  }
  return openShop({retailer_id: retailerID, basket_url: origin});
}

const shopTabs = new Map();
function shopTab(retailerID, origin) {
  if (!shopTabs.has(retailerID)) shopTabs.set(retailerID, findShopTab(retailerID, origin).finally(() => shopTabs.delete(retailerID)));
  return shopTabs.get(retailerID);
}

async function runCommand(settings, command) {
  let reply;
  try {
    const origin = ShopItAdapters.origins[command.retailer_id];
    if (!origin || !['session', 'search'].includes(command.operation)) throw new Error('Unsupported store operation.');
    const tabID = await shopTab(command.retailer_id, origin);
    if (command.operation === 'session') await extension.tabs.update(tabID, {active: true});
    const result = await storeCall(tabID, command.retailer_id, command.operation, {input: command.input});
    reply = {result};
  } catch (error) { reply = {error: error.message, status: error.status || 'failed'}; }
  await api(settings, `/v1/client/commands/${encodeURIComponent(command.id)}/result`, 'PUT', reply);
}

function journal(settings) {
  const key = (id) => `receipt:${settings.url}:${settings.client_id}:${id}`;
  return {
    load: async (id) => (await storage.get(key(id)))[key(id)],
    save: (id, record) => storage.set({[key(id)]: record}),
  };
}

async function transfer(settings, input) {
  const activeKey = `active:${settings.url}:${settings.client_id}`;
  await storage.set({[activeKey]: input});
  let leaseError;
  const renew = async () => {
    if (leaseError) throw leaseError;
    await api(settings, `/v1/client/transfers/${input.id}/renew`);
  };
  const heartbeat = setInterval(() => renew().catch((error) => { leaseError = error; }), 10000);
  const name = ShopItAdapters.names[input.retailer_id], total = input.items.length;
  let tabID, openError, receipt;
  const overlay = (state) => tabID === undefined ? Promise.resolve() : storeCall(tabID, input.retailer_id, 'overlay', {state}).catch(() => {});
  try {
    try {
      tabID = await openShop(input);
      if (input.context.fulfillment) {
        const current = await storeCall(tabID, input.retailer_id, 'session');
        if (current.status !== 'ready' || JSON.stringify(current.context) !== JSON.stringify(input.context)) throw new Error('Your store settings changed. Review a fresh comparison before filling this basket.');
      }
    } catch (error) { openError = error; }
    const call = (operation, body) => openError ? Promise.reject(openError) : storeCall(tabID, input.retailer_id, operation, body);
    const progress = (index) => overlay({title: `Filling your ${name} basket`, detail: `Adding ${input.items[index].name || 'item'}`, progress: index / total, count: `Item ${index + 1} of ${total}`});
    receipt = await applyTransfer(input, {read: () => call('read'), set: (item, before) => call('set', {item, before})}, journal(settings), renew, progress);
  } finally { clearInterval(heartbeat); }
  const added = receipt.lines.filter((line) => line.status === 'applied').length;
  await overlay({title: added === total ? 'Your basket is filled' : 'Some items need review', detail: added === total ? `All ${total} items are in your ${name} basket.` : 'Shop It lists the items that were not added.', progress: added / total, count: `${added} of ${total} added`, finished: true});
  await api(settings, `/v1/client/transfers/${input.id}/result`, 'PUT', receipt);
  await storage.remove(activeKey);
  if (tabID !== undefined) {
    await new Promise((resolve) => setTimeout(resolve, 2500));
    await extension.tabs.reload(tabID).catch(() => overlay(null));
  }
  basket = `Last basket: ${added === total ? 'all items added' : 'some items need review'}.`;
}

async function tick() {
  try {
    const {settings} = await storage.get('settings');
    if (!settings) return false;
    const activeKey = `active:${settings.url}:${settings.client_id}`;
    const active = (await storage.get(activeKey))[activeKey];
    if (active) {
      const saved = await journal(settings).load(active.id);
      if (saved?.receipt) {
        await api(settings, `/v1/client/transfers/${active.id}/result`, 'PUT', saved.receipt);
        await storage.remove(activeKey);
      }
    }
    const {commands} = await api(settings, '/v1/client/commands?wait=20');
    await Promise.all(commands.map((command) => runCommand(settings, command)));
    const input = await api(settings, '/v1/client/claims');
    connection = 'Connected to Shop It.';
    if (input) await transfer(settings, input);
  } catch (error) { connection = error.message; }
  return true;
}

async function poll() {
  if (polling) return;
  polling = true;
  try {
    for (let started = Date.now(); await tick(); started = Date.now()) {
      await new Promise((resolve) => setTimeout(resolve, started + 1000 - Date.now()));
    }
  } finally { polling = false; }
}

function pageOrigin(sender) {
  const url = new URL(sender.url);
  if (!['/', '/plugin'].includes(url.pathname)) throw new Error('Connect from the Shop It page.');
  return localURL(url.origin);
}

async function pair(message, sender) {
  const url = localURL(message.url);
  if (pageOrigin(sender) !== url) throw new Error('Pair from the Shop It page.');
  const credentials = {url, token: message.token};
  const {clients} = await api(credentials, '/v1/clients', 'GET');
  const client = clients.find((client) => client.id === 'browser') || clients[0];
  if (!client) throw new Error('No browser client is configured.');
  const paired = await api(credentials, `/v1/clients/${encodeURIComponent(client.id)}/pair`);
  await storage.set({settings: {url, token: paired.token, client_id: paired.id}});
  void poll();
  return {client_id: paired.id};
}

extension.runtime.onMessage.addListener((message, sender, respond) => {
  if (sender.id !== extension.runtime.id) return false;
  if (message.type === 'pair' && sender.tab) {
    pair(message, sender).then(respond, (error) => respond({error: error.message}));
    return true;
  }
  if (message.type === 'fill' && sender.tab) {
    try { pageOrigin(sender); } catch { return false; }
    void poll();
    return false;
  }
  if (message.type === 'connection' && sender.tab) {
    let url;
    try { url = pageOrigin(sender); } catch { return false; }
    storage.get('settings').then(({settings}) => respond(settings?.url === url ? {connected: true, client_id: settings.client_id} : {connected: false}));
    return true;
  }
  if (message.type === 'status' && sender.url?.startsWith(extension.runtime.getURL(''))) {
    storage.get('settings').then(({settings}) => respond({status: [connection, basket].filter(Boolean).join(' '), url: settings?.url || 'http://127.0.0.1:24816'}));
    return true;
  }
  return false;
});

extension.alarms.create('shop-it-poll', {periodInMinutes: 0.5});
extension.alarms.onAlarm.addListener(() => { void poll(); });
void poll();
