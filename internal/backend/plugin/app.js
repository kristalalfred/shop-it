'use strict';
(() => {
  const root = document.getElementById('app');
  const host = window.parent !== window;
  const pending = new Map();
  let sequence = 0, state = null, view = '', busy = '', error = '', draft = null, poll;
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
    const oldPlan = state?.plan?.id;
    state = value;
    if (draft === null) draft = state.preferences.draft || '';
    if (!view || state.plan?.id && oldPlan !== state.plan.id) view = state.plan ? 'plan' : !state.preferences.draft ? 'request' : !state.preferences.retailer_ids.length ? 'stores' : 'connect';
    render();
    if (host) rpc('ui/update-model-context', {content: [{type: 'text', text: JSON.stringify({plan_id: state.plan?.id, revision: state.plan?.revision, retailer_ids: state.preferences.retailer_ids, draft: state.preferences.draft, setup_ready: ready()})}]}).catch(() => {});
  }
  async function action(label, work) {
    if (busy) return;
    busy = label; error = ''; render();
    try { await work(); } catch (e) { error = e.message; }
    finally { busy = ''; render(); }
  }
  const selected = () => state.stores.filter((store) => store.selected);
  const isReady = (store) => store.mode !== 'browser' || store.session?.status === 'ready';
  const ready = () => selected().length > 0 && selected().every(isReady);
  const shopName = (id) => state.stores.find((s) => s.id === id)?.name || id;
  function saveRequest() {
    return action('Saving your list', async () => { if (!draft?.trim()) throw new Error('Add a shopping list or a meal idea.'); accept(await call('configure_shop_it', {draft})); view = 'stores'; });
  }
  function header(shell) {
    const h = node('header', null, 'header'), brand = node('div', null, 'brand'); brand.append(node('span', '↗', 'mark'), node('span', 'Shop It'));
    const right = node('div', null, 'right');
    right.append(node('span', state.demo ? 'Demo stores' : state.browser_online ? 'Browser connected' : 'Browser offline', `badge${state.browser_online || state.demo ? ' live' : ''}`));
    if (state.plan && view === 'plan') right.append(button('Stores', () => { view = 'stores'; error = ''; render(); }, 'quiet small'));
    h.append(brand, right); shell.append(h);
  }
  function stepper(shell) {
    const current = ['request', 'stores', 'connect'].indexOf(view);
    const steps = node('nav', null, 'steps'); steps.setAttribute('aria-label', 'Setup progress');
    ['Your list', 'Your stores', 'Connect'].forEach((name, index) => {
      const step = node('span', null, `step${index === current ? ' current' : index < current ? ' done' : ''}`);
      if (index === current) step.setAttribute('aria-current', 'step');
      step.append(node('b', index < current ? '✓' : index + 1), node('span', name)); steps.append(step);
    }); shell.append(steps);
  }
  function intro(parent, eyebrow, title, subtitle) { parent.append(node('div', eyebrow, 'eyebrow'), node('h1', title)); if (subtitle) parent.append(node('p', subtitle, 'subtle')); }
  function requestView(parent) {
    intro(parent, 'A little less life admin', 'From idea to basket.', 'Plan with ChatGPT. Shop at your stores.');
    const form = node('form', null, 'request'), input = node('textarea'); input.value = draft || ''; input.placeholder = 'Five easy dinners for two. Vegetarian, around 800 kr.'; input.maxLength = 16000; input.required = true; input.setAttribute('aria-label', 'Your shopping request');
    input.addEventListener('input', () => { draft = input.value; });
    const footer = node('div', null, 'request-footer'); footer.append(node('span', 'Meals, a recipe, or a grocery list', 'subtle'), button('Continue ↗', saveRequest, 'primary'));
    form.addEventListener('submit', (event) => { event.preventDefault(); saveRequest(); }); form.append(input, footer); parent.append(form);
    const examples = node('div', null, 'examples');
    for (const [label, value] of [['Weeknight dinners', 'Five easy vegetarian dinners for two, around 800 kr.'], ['Breakfast basics', '2 litres of milk, 12 eggs and 500 g of bananas.'], ['A recipe', 'Korvstroganoff för 5 personer. Vi har salt och peppar.']]) examples.append(button(label, () => { draft = value; input.value = value; input.focus(); }));
    parent.append(examples);
    if (state.demo) parent.append(button('Try a sample comparison', sample, 'quiet'));
  }
  function storesView(parent) {
    intro(parent, 'Shop where you shop', 'Your usual stores.', 'Choose one or compare a few.');
    const grid = node('div', null, 'store-grid');
    for (const store of state.stores) {
      const card = button('', () => action('Saving stores', async () => {
        const ids = selected().map((s) => s.id).filter((id) => id !== store.id); if (!store.selected) ids.push(store.id);
        accept(await call('configure_shop_it', {retailer_ids: ids}));
      }), `store-card${store.selected ? ' selected' : ''}`);
      card.dataset.store = store.id; card.setAttribute('aria-pressed', String(store.selected)); card.setAttribute('aria-label', store.name);
      card.append(node('span', store.selected ? '✓' : '+', 'check'), node('span', store.name, 'store-symbol'), node('span', store.mode === 'browser' ? 'Your store session' : 'Demo catalogue', 'subtle')); grid.append(card);
    }
    parent.append(grid);
    const bottom = node('div', null, 'bottom'); bottom.append(button('← Your list', () => { view = 'request'; render(); }, 'quiet'), button('Connect stores ↗', () => { view = 'connect'; render(); }, 'primary', !selected().length)); parent.append(bottom);
  }
  function installView(parent) {
    const box = node('section', null, 'install'); box.append(node('h2', 'Connect your browser'));
    box.append(link('Open browser setup ↗', state.setup_url, 'button primary'), node('p', 'Your store logins stay in your browser.', 'subtle'));
    parent.append(box);
  }
  function connectView(parent) {
    intro(parent, 'One quick connection', ready() ? 'You’re ready to shop.' : 'Bring your stores along.', ready() ? 'Your request is saved. Let’s find the groceries.' : 'Your store logins stay in your browser.');
    if (!state.browser_online && !state.demo) installView(parent);
    const list = node('div', null, 'connection-list');
    for (const store of selected()) {
      const row = node('div', null, 'connection-row'), copy = node('div', null, 'copy');
      const status = isReady(store) ? store.session?.label || 'Ready' : store.session?.status === 'login_required' ? 'Sign in at the store, then check again' : store.session?.status === 'setup_required' ? 'Choose your store or delivery, then check again' : store.session?.status === 'offline' ? 'Reconnect your browser' : 'Ready to connect';
      copy.append(node('strong', store.name), node('span', status, 'subtle'));
      row.append(node('span', '', `dot${isReady(store) ? ' ready' : ''}`), copy);
      if (store.session && !isReady(store)) row.append(link('Open store ↗', store.url, 'button small'));
      row.append(button(isReady(store) ? 'Check again' : store.session ? 'Check again' : 'Connect', () => action(`Checking ${store.name}`, async () => { accept(await call('connect_store', {retailer_id: store.id})); }), 'small', store.mode === 'browser' && !state.browser_online));
      list.append(row);
    }
    parent.append(list);
    const resumePlan = state.plan && selected().some((store) => store.id === state.plan.retailer_id);
    const bottom = node('div', null, 'bottom'); bottom.append(button('← Your stores', () => { view = 'stores'; render(); }, 'quiet'), button(resumePlan ? 'Back to basket' : 'Find my groceries ↗', () => action('Opening your shopping request', async () => {
      if (resumePlan) { view = 'plan'; return; }
      await rpc('ui/message', {role: 'user', content: [{type: 'text', text: `Use Shop It to plan this request with my connected stores: ${state.preferences.draft || draft}. Create a shopping plan with concrete ingredients, then choose matching products from the returned candidates. Leave uncertain choices for me to review.`}]});
    }), 'primary', !ready())); parent.append(bottom);
  }
  async function sampleWork() {
    const choices = selected().filter((s) => s.mode === 'mock');
    const ids = (choices.length ? choices : state.stores.filter((s) => s.mode === 'mock')).map((s) => s.id);
    accept(await call('configure_shop_it', {retailer_ids: ids, draft: '2 litres of milk, 12 eggs and 500 g of bananas.'}));
    accept(await call('create_shopping_plan', {text: 'Breakfast basics', retailer_ids: ids, items: [{label: 'Milk', query: 'mjölk', amount: {quantity: 2000, unit: 'ml'}, required_traits: []}, {label: 'Eggs', query: 'ägg', amount: {quantity: 12, unit: 'each'}, required_traits: []}, {label: 'Bananas', query: 'banan', amount: {quantity: 500, unit: 'g'}, required_traits: []}]}));
    const productIDs = ['milk-basic', 'eggs', 'bananas'];
    const selections = state.plan.lines.flatMap((line, i) => ids.filter((id) => line.baskets[id]?.candidates.some((c) => c.eligible && c.product.id === productIDs[i])).map((id) => ({line_id: line.id, retailer_id: id, product_id: productIDs[i]})));
    if (selections.length) await mutate('select_products', {selections});
  }
  function sample() { return action('Comparing demo groceries', sampleWork); }
  async function mutate(name, args) { accept(await call(name, {plan_id: state.plan.id, revision: state.plan.revision, ...args})); }
  function planView(shell) {
    const plan = state.plan, basket = plan.baskets.find((b) => b.retailer_id === plan.retailer_id);
    const top = node('div', null, 'plan-header'), copy = node('div', null, 'copy'); copy.append(node('h1', 'A basket that fits.', 'plan-title'), node('p', plan.text, 'subtle')); top.append(copy, button('New list +', () => { view = 'request'; draft = ''; error = ''; render(); }, 'small')); shell.append(top);
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
    if (!connected) checkout.append(button('Reconnect store', () => { view = 'connect'; render(); }, 'small'));
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
    if (view === 'plan' && state.plan) planView(shell);
    else {
      stepper(shell);
      const panel = node('section', null, 'onboarding');
      ({request: requestView, stores: storesView, connect: connectView}[view] || requestView)(panel); shell.append(panel);
    }
    if (busy) { const status = node('div', null, 'notice'); status.setAttribute('role', 'status'); status.append(node('span', '', 'spinner'), node('span', ` ${busy}…`)); shell.append(status); }

    root.replaceChildren(shell);
    if (view === 'plan') [...root.querySelectorAll('details.grocery')].forEach((n, i) => { n.open = open.includes(i); });
    if (host) notify('ui/notifications/size-changed', {height: document.documentElement.scrollHeight});
  }
  async function refresh() {
    if (busy || document.activeElement?.matches('textarea,input,select')) return;
    try { const next = await call('open_shop_it', state?.plan && view === 'plan' ? {plan_id: state.plan.id} : {}); if (JSON.stringify(next) !== JSON.stringify(state)) accept(next); } catch {}
  }
  async function boot() {
    try {
      if (!host) throw new Error('Open Shop It from ChatGPT.');
      const initialized = await rpc('ui/initialize', {protocolVersion: '2026-01-26', appInfo: {name: 'Shop It', version: '0.3.0'}, appCapabilities: {}}, 15000);
      applyTheme(initialized.hostContext); notify('ui/notifications/initialized', {});
      accept(await call('open_shop_it'));
      poll = setInterval(refresh, 4000);
    } catch (e) { root.replaceChildren(node('div', e.message, 'error'), button('Try again', boot, 'primary')); }
  }
  window.addEventListener('pagehide', () => { clearInterval(poll); for (const request of pending.values()) request.reject(new Error('Shop It closed.')); pending.clear(); });
  boot();
})();
