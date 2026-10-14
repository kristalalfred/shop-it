import {api, applyTransfer, localURL} from './runner.js';
import './adapters.js';

const extension = globalThis.browser || globalThis.chrome;
const origins = ShipItAdapters.origins;
let running = false;
let sessions = [];
let sessionTime = 0;
let status = 'Connect from the Ship It page.';
const storage = extension.storage.local;
if (storage.setAccessLevel) storage.setAccessLevel({accessLevel: 'TRUSTED_CONTEXTS'});

async function storeCall(tabID, retailerID, operation, body = {}) {
  const reply = await extension.tabs.sendMessage(tabID, {type: 'store-operation', retailer_id: retailerID, operation, ...body});
  if (!reply || reply.error) throw Object.assign(new Error(reply?.error || 'Reload your store tab to connect it.'), {status: reply?.status});
  return reply.result;
}

async function discover(settings) {
  const found = [];
  const issues = [];
  const tabs = await extension.tabs.query({url: Object.values(origins).map((origin) => origin + '/*')});
  for (const [id, origin] of Object.entries(origins)) {
    const tab = tabs.find((tab) => { try { return new URL(tab.url).origin === origin; } catch { return false; } });
    if (!tab) continue;
    try {
      const session = await storeCall(tab.id, id, 'session');
      found.push({...session, tabID: tab.id});
    } catch (error) { issues.push(error.message); }
  }
  await api(settings, '/v1/client/sessions', 'POST', {sessions: found.map(({tabID, ...session}) => session)});
  sessions = found;
  sessionTime = Date.now();
  status = issues.join(' ') || (sessions.length ? `${sessions.length} store session${sessions.length === 1 ? '' : 's'} connected.` : 'Open Mathem, Willys or Hemköp and choose your store.');
}

function findSession(retailerID, context) {
  const session = sessions.find((session) => session.retailer_id === retailerID && ShipItAdapters.sameContext(session.context, context));
  if (!session) throw new Error('The matching store tab is unavailable.');
  return session;
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
  const receipts = journal(settings);
  let session;
  try { session = findSession(input.retailer_id, input.context); } catch {}
  const call = (operation, body) => {
    if (!session) throw new Error('The matching store tab is unavailable.');
    return storeCall(session.tabID, input.retailer_id, operation, body);
  };
  let leaseError;
  const renew = async () => {
    if (leaseError) throw leaseError;
    await api(settings, `/v1/client/transfers/${input.id}/renew`);
  };
  const heartbeat = setInterval(() => renew().catch((error) => { leaseError = error; }), 10000);
  let receipt;
  try {
    receipt = await applyTransfer(input, {read: (context) => call('read', {context}), set: (context, item, before) => call('set', {context, item, before})}, receipts, renew);
  } finally { clearInterval(heartbeat); }
  await api(settings, `/v1/client/transfers/${input.id}/result`, 'PUT', receipt);
  await storage.remove(activeKey);
  status = `Basket result: ${receipt.lines.every((line) => line.status === 'applied') ? 'confirmed' : 'review needed'}.`;
}

async function tick() {
  if (running) return;
  running = true;
  try {
    const {settings} = await storage.get('settings');
    if (!settings?.enabled) return;
    if (Date.now() - sessionTime > 15000) await discover(settings);
    const {jobs} = await api(settings, '/v1/client/search-claims');
    await Promise.all(jobs.map(async (job) => {
      let reply;
      try {
        const session = findSession(job.retailer_id, job.context);
        reply = {result: await storeCall(session.tabID, job.retailer_id, 'search', {job})};
      } catch { reply = {error: 'adapter unavailable'}; }
      try { await api(settings, `/v1/client/searches/${job.id}/result`, 'PUT', reply); } catch (error) { status = error.message; }
    }));
    const activeKey = `active:${settings.url}:${settings.client_id}`;
    const active = (await storage.get(activeKey))[activeKey];
    if (active) {
      const saved = await journal(settings).load(active.id);
      if (saved?.receipt) {
        await api(settings, `/v1/client/transfers/${active.id}/result`, 'PUT', saved.receipt);
        await storage.remove(activeKey);
      }
    }
    const input = await api(settings, '/v1/client/claims');
    if (input) await transfer(settings, input);
  } catch (error) { status = error.message; }
  finally { running = false; }
}

async function pair(message, sender) {
  const url = localURL(message.url);
  if (new URL(sender.url).origin !== url || new URL(sender.url).pathname !== '/') throw new Error('Pair from the Ship It page.');
  const credentials = {url, token: message.token};
  const {clients} = await api(credentials, '/v1/clients', 'GET');
  const client = clients.find((client) => client.id === 'browser') || clients[0];
  if (!client) throw new Error('No browser client is configured.');
  const paired = await api(credentials, `/v1/clients/${encodeURIComponent(client.id)}/pair`);
  await storage.set({settings: {url, token: paired.token, client_id: paired.id, enabled: true}});
  sessions = [];
  sessionTime = 0;
  void tick();
  return {client_id: paired.id};
}

extension.runtime.onMessage.addListener((message, sender, respond) => {
  if (sender.id !== extension.runtime.id) return false;
  const fromExtension = sender.url?.startsWith(extension.runtime.getURL(''));
  if (message.type === 'pair' && sender.tab) {
    pair(message, sender).then(respond, (error) => respond({error: error.message}));
    return true;
  }
  if (!fromExtension) return false;
  if (message.type === 'status') {
    storage.get('settings').then(({settings}) => respond({status, enabled: Boolean(settings?.enabled), url: settings?.url || 'http://127.0.0.1:18080', sessions: sessions.map(({tabID, context, ...session}) => session)}));
    return true;
  }
  if (message.type === 'enabled') {
    storage.get('settings').then(async ({settings}) => {
      if (!settings) throw new Error('Connect from the Ship It page first.');
      if (running) throw new Error('Wait for the current operation to finish.');
      await storage.set({settings: {...settings, enabled: Boolean(message.enabled)}});
      if (!message.enabled) await api(settings, '/v1/client/sessions', 'POST', {sessions: []});
      sessionTime = 0;
      respond({ok: true});
      void tick();
    }).catch((error) => respond({error: error.message}));
    return true;
  }
  return false;
});

extension.alarms.create('ship-it-poll', {periodInMinutes: 0.5});
extension.alarms.onAlarm.addListener(() => { void tick(); });
setInterval(() => { void tick(); }, 1000);
void tick();
