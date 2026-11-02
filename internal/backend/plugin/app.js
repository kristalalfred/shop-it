'use strict';
(() => {
  const root = document.getElementById('app');
  const host = window.parent !== window;
  const pending = new Map();
  let sequence = 0, state = null, busy = '', error = '', poll;
  const money = (value) => new Intl.NumberFormat('sv-SE', {style: 'currency', currency: value.currency, maximumFractionDigits: 2}).format(value.amount_minor / 100);
  const node = (tag, text, className) => { const n = document.createElement(tag); if (text != null) n.textContent = text; if (className) n.className = className; return n; };
  function button(text, fn, className = '', disabled = false) {
    const n = node('button', text, className); n.type = 'button'; n.disabled = disabled || Boolean(busy); n.addEventListener('click', fn); return n;
  }
  function link(text, url, className = 'button') {
    const a = node('a', text, className); a.href = url; a.target = '_blank'; a.rel = 'noopener noreferrer';
    if (host) a.addEventListener('click', (event) => { event.preventDefault(); rpc('ui/open-link', {url}).catch((e) => { error = e.message; render(); }); });
    return a;
  }
  function rpc(method, params, timeout = 120000) {
    return new Promise((resolve, reject) => {
      const id = ++sequence;
      const timer = setTimeout(() => { pending.delete(id); reject(new Error('The connection timed out. Try again.')); }, timeout);
      pending.set(id, {resolve: (value) => { clearTimeout(timer); resolve(value); }, reject: (e) => { clearTimeout(timer); reject(e); }});
      window.parent.postMessage({jsonrpc: '2.0', id, method, params}, '*');
    });
  }
  function notify(method, params) { window.parent.postMessage({jsonrpc: '2.0', method, params}, '*'); }
  window.addEventListener('message', (event) => {
    if (!host || event.source !== window.parent || event.data?.jsonrpc !== '2.0') return;
    const message = event.data;
    if (message.id != null && pending.has(message.id)) {
      const request = pending.get(message.id); pending.delete(message.id);
      if (message.error) request.reject(new Error(message.error.message)); else request.resolve(message.result);
    } else if (message.method === 'ui/notifications/tool-result') {
      const value = message.params?.structuredContent;
      if (value?.stores && value.preferences) accept(value);
    } else if (message.method === 'ui/notifications/host-context-changed') {
      applyTheme(message.params);
    }
  });
  function applyTheme(context) {
    if (context?.theme === 'dark') document.documentElement.dataset.hostTheme = 'dark';
    else delete document.documentElement.dataset.hostTheme;
  }
  async function call(name, args = {}) {
    const result = await rpc('tools/call', {name, arguments: args});
    if (result.isError) throw new Error(result.content?.find((c) => c.type === 'text')?.text || 'Shop It could not complete this action.');
    return result.structuredContent;
  }
  function accept(value) {
    if (!value?.preferences || !Array.isArray(value.stores)) return;
    state = value;
    render();
    if (host) rpc('ui/update-model-context', {content: [{type: 'text', text: JSON.stringify({plan_id: state.plan?.id, revision: state.plan?.revision, retailer_ids: state.preferences.retailer_ids, setup_ready: ready(), setup_url: state.setup_url})}]}).catch(() => {});
  }
  async function action(label, work) {
    if (busy) return;
    busy = label; error = ''; render();
    try { await work(); } catch (e) { error = e.message; }
    finally { busy = ''; render(); }
  }
  const selected = () => state.stores.filter((store) => store.selected);
  const isReady = (store) => store.mode !== 'browser' || store.session?.status === 'ready';
  const browserReady = () => state.demo || state.browser_online;
  const ready = () => browserReady() && selected().length > 0 && selected().some(isReady);
  const shopName = (id) => state.stores.find((s) => s.id === id)?.name || id;
  function header(shell) {
    const h = node('header', null, 'header'), brand = node('div', null, 'brand'); brand.append(node('span', '↗', 'mark'), node('span', 'Shop It'));
    const right = node('div', null, 'right');
    right.append(node('span', state.demo ? 'Demo stores' : state.browser_online ? 'Browser connected' : 'Browser offline', `badge${browserReady() ? ' live' : ''}`));
    h.append(brand, right); shell.append(h);
  }
  const storeStatus = (store) => isReady(store) ? store.session?.label || 'Ready' : store.session?.status === 'login_required' ? 'Sign in' : store.session?.status === 'setup_required' ? 'Choose delivery or pickup' : 'Not checked';
  function statusView(shell) {
    const panel = node('section', null, 'status');
    const rows = node('div', null, 'connection-list');
    if (!state.demo) {
      const row = node('div', null, 'connection-row'), copy = node('div', null, 'copy');
      copy.append(node('strong', 'Browser extension'), node('span', state.browser_online ? 'Connected' : 'Not connected', 'subtle'));
      row.append(node('span', '', `dot${state.browser_online ? ' ready' : ''}`), copy); rows.append(row);
    }
    for (const store of selected()) {
      const row = node('div', null, 'connection-row'), copy = node('div', null, 'copy');
      copy.append(node('strong', store.name), node('span', storeStatus(store), 'subtle'));
      row.append(node('span', '', `dot${isReady(store) ? ' ready' : ''}`), copy);
      if (!isReady(store)) row.append(button('Check', () => action(`Checking ${store.name}`, async () => { accept(await call('connect_store', {retailer_id: store.id})); }), 'small', !browserReady()));
      rows.append(row);
    }
    if (!selected().length) {
      const row = node('div', null, 'connection-row'), copy = node('div', null, 'copy');
      copy.append(node('strong', 'Stores'), node('span', 'None chosen', 'subtle'));
      row.append(node('span', '', 'dot'), copy); rows.append(row);
    }
    panel.append(rows);
    if (!ready()) panel.append(link('Finish setup ↗', state.setup_url, 'button primary'));
    shell.append(panel);
  }
  async function mutate(name, args) { accept(await call(name, {plan_id: state.plan.id, revision: state.plan.revision, ...args})); }
  function planView(shell) {
    const plan = state.plan, basket = plan.baskets.find((b) => b.retailer_id === plan.retailer_id);
    const top = node('div', null, 'plan-header'), copy = node('div', null, 'copy'); copy.append(node('h1', 'A basket that fits.', 'plan-title'), node('p', plan.text, 'subtle')); top.append(copy); shell.append(top);
    const comparisons = node('div', null, 'comparison');
    for (const b of plan.baskets) {
      const card = button('', () => action('Choosing store', () => mutate('select_store', {retailer_id: b.retailer_id})), `basket-card${b.retailer_id === plan.retailer_id ? ' selected' : ''}`);
      card.setAttribute('aria-pressed', String(b.retailer_id === plan.retailer_id)); card.append(node('span', shopName(b.retailer_id)), node('strong', b.missing === plan.lines.filter((line) => !line.skipped).length && b.missing > 0 ? 'Choose products' : money(b.total)), node('span', b.missing ? `${b.missing} to choose` : 'Everything found', 'subtle')); comparisons.append(card);
    }
    shell.append(comparisons);
    const body = node('div', null, 'plan-body'), list = node('div', null, 'grocery-list');
    for (const line of plan.lines) list.append(grocery(line));
    const checkout = node('aside', null, 'checkout'); checkout.append(node('h3', `${shopName(plan.retailer_id)} subtotal`), node('div', money(basket.total), 'amount'), node('p', `${state.demo ? 'Demo prices. ' : ''}${basket.estimated ? 'Estimated by weight. ' : ''}Delivery excluded.`, 'subtle'));
    const retailer = state.stores.find((store) => store.id === plan.retailer_id);
    const connected = retailer && isReady(retailer);
    const contextChanged = connected && retailer.mode === 'browser' && JSON.stringify(retailer.session.context) !== JSON.stringify(basket.context);
    if (basket.missing) checkout.append(node('p', `Choose or skip ${basket.missing === 1 ? '1 remaining grocery' : `${basket.missing} remaining groceries`}.`, 'subtle'));
    if (!connected && retailer) checkout.append(button(`Check ${retailer.name}`, () => action(`Checking ${retailer.name}`, async () => { accept(await call('connect_store', {retailer_id: retailer.id})); }), 'small', !browserReady()));
    if (contextChanged) checkout.append(button('Refresh for this store', () => action('Refreshing prices', () => mutate('refresh_plan', {})), 'primary'));
    checkout.append(link('Review and fill ↗', state.review_url, contextChanged ? 'button small' : 'button primary'));
    body.append(list, checkout); shell.append(body);
    const footer = node('div', null, 'plan-footer'); footer.append(button('↻ Refresh prices', () => action('Refreshing prices', () => mutate('refresh_plan', {})), 'quiet small'), node('span', `Saved · revision ${plan.revision}`, 'subtle')); shell.append(footer);
  }
  function grocery(line) {
    const plan = state.plan, entry = line.baskets[plan.retailer_id], selectedProduct = entry?.selected;
    const detail = node('details', null, `grocery${line.skipped ? ' skipped' : ''}`), summary = node('summary'), copy = node('div', null, 'copy');
    copy.append(node('strong', line.item.label), node('div', line.skipped ? 'Skipped' : selectedProduct ? `${selectedProduct.target_quantity} × ${selectedProduct.product.name}` : `${line.item.amount.quantity} ${line.item.amount.unit}`, 'detail'));
    summary.append(copy, selectedProduct && !line.skipped ? node('span', money(selectedProduct.total), 'price') : node('span', line.skipped ? 'Skipped' : 'Choose', 'flag')); detail.append(summary);
    const editor = node('div', null, 'grocery-editor');
    if (selectedProduct?.unmet_traits?.length) editor.append(node('p', `Unverified: ${selectedProduct.unmet_traits.join(', ').replaceAll('_', ' ')}`, 'subtle'));
    const form = node('form', null, 'edit-row'), query = node('input'), quantity = node('input'), unit = node('select');
    query.value = line.item.query; query.required = true; query.maxLength = 200; query.setAttribute('aria-label', `Search for ${line.item.label}`);
    quantity.type = 'number'; quantity.min = 1; quantity.max = 1000000; quantity.step = 1; quantity.value = line.item.amount.quantity; quantity.required = true; quantity.className = 'quantity'; quantity.setAttribute('aria-label', 'Quantity');
    for (const value of ['g', 'ml', 'each', 'pack']) { const option = node('option', value); option.value = value; unit.append(option); } unit.value = line.item.amount.unit; unit.setAttribute('aria-label', 'Unit');
    const search = button('Search', () => { if (form.reportValidity()) action('Searching groceries', () => mutate('update_plan_line', {line_id: line.id, item: {...line.item, query: query.value.trim(), amount: {quantity: Number(quantity.value), unit: unit.value}}})); }, 'small');
    form.addEventListener('submit', (event) => { event.preventDefault(); search.click(); }); form.append(query, quantity, unit, search); editor.append(form);
    for (const candidate of entry?.candidates || []) {
      const chosen = candidate.product.id === selectedProduct?.product.id;
      const option = button('', () => action('Choosing grocery', () => mutate('update_plan_line', {line_id: line.id, product_id: candidate.product.id})), `candidate${chosen ? ' selected' : ''}${!candidate.eligible ? ' ineligible' : ''}`, !candidate.eligible);
      option.setAttribute('aria-pressed', String(chosen)); const text = node('span'); text.append(node('span', candidate.product.name), node('div', `${candidate.product.pack.quantity} ${candidate.product.pack.unit} · ${candidate.target_quantity} packs${candidate.unmet_traits?.length ? ' · Requirements unverified' : ''}${!candidate.eligible ? ' · Unavailable or incompatible size' : ''}`, 'detail')); option.append(text, node('strong', money(candidate.total))); editor.append(option);
    }
    if (!entry?.candidates?.length) editor.append(node('p', entry?.issue || 'No matching products found.', 'subtle'));
    editor.append(button(line.skipped ? 'Restore grocery' : 'Skip grocery', () => action('Updating grocery', () => mutate('update_plan_line', {line_id: line.id, skipped: !line.skipped})), 'quiet small')); detail.append(editor); return detail;
  }
  function render() {
    if (!state) return;
    const open = [...root.querySelectorAll('details.grocery')].map((n, index) => n.open ? index : -1).filter((index) => index >= 0);
    const shell = node('main', null, 'shell'); header(shell);
    if (error) { const status = node('div', error, 'error'); status.setAttribute('role', 'alert'); status.append(button('Refresh connection', () => action('Reconnecting', async () => accept(await call('open_shop_it'))), 'quiet small')); shell.append(status); }
    if (state.plan) planView(shell);
    else statusView(shell);
    if (busy) { const status = node('div', null, 'notice'); status.setAttribute('role', 'status'); status.append(node('span', '', 'spinner'), node('span', ` ${busy}…`)); shell.append(status); }

    root.replaceChildren(shell);
    if (state.plan) [...root.querySelectorAll('details.grocery')].forEach((n, i) => { n.open = open.includes(i); });
    if (host) notify('ui/notifications/size-changed', {height: document.documentElement.scrollHeight});
  }
  async function refresh() {
    if (busy || document.activeElement?.matches('textarea,input,select')) return;
    try { const next = await call('open_shop_it', state?.plan ? {plan_id: state.plan.id} : {}); if (JSON.stringify(next) !== JSON.stringify(state)) accept(next); } catch {}
  }
  async function boot() {
    try {
      if (!host) throw new Error('Open Shop It from ChatGPT.');
      const initialized = await rpc('ui/initialize', {protocolVersion: '2026-01-26', appInfo: {name: 'Shop It', version: '{{VERSION}}'}, appCapabilities: {}}, 15000);
      applyTheme(initialized.hostContext); notify('ui/notifications/initialized', {});
      accept(await call('open_shop_it'));
      poll = setInterval(refresh, 4000);
    } catch (e) { root.replaceChildren(node('div', e.message, 'error'), button('Try again', boot, 'primary')); }
  }
  window.addEventListener('pagehide', () => { clearInterval(poll); for (const request of pending.values()) request.reject(new Error('Shop It closed.')); pending.clear(); });
  boot();
})();
