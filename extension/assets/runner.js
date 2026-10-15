export async function applyTransfer(transfer, adapter, journal, renew) {
  const fingerprint = JSON.stringify({retailer_id: transfer.retailer_id, context: transfer.context, items: transfer.items});
  let record = await journal.load(transfer.id);
  if (record && record.fingerprint !== fingerprint) throw new Error('Transfer payload changed.');
  record ||= {fingerprint, lines: {}};
  if (record.receipt) return record.receipt;
  const save = () => journal.save(transfer.id, structuredClone(record));
  for (const item of transfer.items) {
    let line = record.lines[item.product_id];
    if (line?.result) continue;
    let started = line?.phase === 'started';
    let result;
    try {
      const basket = await adapter.read(transfer.context);
      const current = basket[item.product_id] || 0;
      if (current === item.target_quantity) {
        result = {product_id: item.product_id, status: 'applied', confirmed_quantity: current};
      } else if (started) {
        result = {product_id: item.product_id, status: 'uncertain', confirmed_quantity: 0, message: 'An interrupted update could not be confirmed. Review this item at the store.'};
      } else {
        await renew();
        record.lines[item.product_id] = {phase: 'started', before: current};
        await save();
        started = true;
        await adapter.set(transfer.context, item, current);
        const confirmed = (await adapter.read(transfer.context))[item.product_id] || 0;
        result = {product_id: item.product_id, status: confirmed === item.target_quantity ? 'applied' : 'uncertain', confirmed_quantity: confirmed === item.target_quantity ? confirmed : 0, ...(confirmed !== item.target_quantity ? {message: 'The store did not confirm the requested quantity. Review this item at the store.'} : {})};
      }
    } catch (error) {
      result = {product_id: item.product_id, status: started ? 'uncertain' : error.status === 'login_required' ? 'login_required' : 'failed', confirmed_quantity: 0, message: String(error.message || 'Store operation failed.').slice(0, 500)};
    }
    record.lines[item.product_id] = {phase: 'done', result};
    await save();
  }
  record.receipt = {lines: transfer.items.map((item) => record.lines[item.product_id].result)};
  await save();
  return record.receipt;
}

export async function discoverSessions(origins, tabs, call) {
  const origin = (tab) => { try { return new URL(tab.url).origin; } catch { return ''; } };
  const ranked = [...tabs].sort((a, b) => Number(Boolean(a.discarded)) - Number(Boolean(b.discarded)) || (b.lastAccessed || 0) - (a.lastAccessed || 0));
  const found = [], issues = [];
  for (const [id, shopOrigin] of Object.entries(origins)) {
    let issue = '';
    for (const tab of ranked.filter((tab) => origin(tab) === shopOrigin)) {
      try {
        found.push({...await call(tab.id, id), tabID: tab.id});
        issue = '';
        break;
      } catch (error) {
        issue = error.unreachable && issue ? issue : String(error.message || 'The shop tab did not respond.').slice(0, 300);
        if (!error.unreachable) break;
      }
    }
    if (issue) issues.push({retailer_id: id, message: issue});
  }
  return {found, issues};
}

export function localURL(raw) {
  const url = new URL(raw);
  if (url.protocol !== 'http:' || url.hostname !== '127.0.0.1' || url.username || url.password || url.pathname !== '/' || url.search || url.hash) throw new Error('Use an http://127.0.0.1 address for the local service.');
  return url.origin;
}

export async function api(settings, path, method = 'POST', body) {
  const url = localURL(settings.url);
  const response = await fetch(url + path, {method, headers: {Authorization: `Bearer ${settings.token}`, ...(body !== undefined ? {'Content-Type': 'application/json'} : {})}, ...(body !== undefined ? {body: JSON.stringify(body)} : {}), redirect: 'error', signal: AbortSignal.timeout(25000)});
  if (response.status === 204) return null;
  const result = await response.json();
  if (!response.ok) throw new Error(result.error || `Service returned ${response.status}.`);
  return result;
}
