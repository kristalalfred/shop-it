'use strict';

const $ = (id) => document.getElementById(id);
const money = (m) => new Intl.NumberFormat('sv-SE', {style: 'currency', currency: m.currency}).format(m.amount_minor / 100);
const el = (tag, text, cls) => { const node = document.createElement(tag); if (text != null) node.textContent = text; if (cls) node.className = cls; return node; };
let token = $('token').value.trim(), plan = null, busy = false, retailers = [], clients = [], poll = null, sessions = [], issues = [], sessionPoll = null, interpreterMode = '', basketRedirect = null, redirectTransfer = null;
let extensionReady = false, extensionConnected = false, pairing = false, extensionError = '', shopOptions = '', shopLinks = '';
let selectedShop = '';
const contextEqual = (a, b) => ['store_id', 'postal_code', 'fulfillment', 'delivery_slot_id', 'session_id'].every((key) => (a[key] || '') === (b[key] || '')) && JSON.stringify(a.memberships || []) === JSON.stringify(b.memberships || []);
function shoppingTarget() {
  const retailer = retailers.find((retailer) => retailer.id === selectedShop);
  if (!retailer) return null;
  if (retailer.mode !== 'browser') return {retailer, context: {store_id: `${retailer.id}-demo`, postal_code: '11122', fulfillment: 'delivery', memberships: retailer.mode === 'mock' ? [`${retailer.id}-demo-member`] : []}, clientID: clients[0]?.id};
  const available = sessions.filter((session) => session.retailer_id === retailer.id);
  const session = available.find((session) => session.logged_in) || available[0];
  return session ? {retailer, context: session.context, clientID: session.client_id, loggedIn: session.logged_in} : null;
}
const connectedShops = () => retailers.filter((retailer) => retailer.mode !== 'browser' || sessions.some((session) => session.retailer_id === retailer.id));
function planClient() {
  if (!plan) return null;
  const retailer = retailers.find((retailer) => retailer.id === plan.retailer_id);
  return retailer?.mode === 'browser' ? sessions.find((session) => session.retailer_id === plan.retailer_id && session.logged_in && contextEqual(session.context, plan.context))?.client_id : clients[0]?.id;
}
const examples = {
  breakfast: 'We need 2 litres of organic milk, 12 eggs and 500 g bananas.',
  dinner: 'Korvstroganoff för 5 personer.',
  diet: '2 litres lactose-free milk\n1 pack gluten-free bread\n500 g bananas',
};

async function api(path, body, method = 'POST', key) {
  const headers = {Authorization: `Bearer ${token}`};
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  if (key) headers['Idempotency-Key'] = key;
  const response = await fetch(path, {method, headers, body: body === undefined ? undefined : JSON.stringify(body)});
  const result = await response.json();
  if (!response.ok) {
    const messages = {400: 'Check your input, quantity and selected product.', 401: 'The connection expired. Reload this page to reconnect.', 409: 'The plan changed or still needs review. Reopen it, resolve all lines, and review the warnings.', 502: 'The AI or shop connection failed. Check your AI login and keep the shop tab open, then try again.', 504: 'Planning ingredients or searching the shop took too long. Please try again.'};
    throw new Error(messages[response.status] || result.error || 'The request failed.');
  }
  return result;
}

function message(text, error = false) {
  $('message').replaceChildren();
  if (text) $('message').append(el('p', text, `notice${error ? ' error' : ''}`));
}

function buttons() {
  $('create').disabled = busy || !token || !shoppingTarget();
  $('pair-extension').disabled = busy || pairing || !token || !extensionReady;
  $('retailer').disabled = busy || !connectedShops().length;
  $('refresh').disabled = busy || !plan;
  $('transfer').disabled = busy || !plan || plan.status !== 'ready' || (plan.warnings.length > 0 && !$('ack').checked) || (plan.over_budget && !$('over').checked) || !planClient();
  $('reconnect').disabled = busy;
}

async function action(work) {
  if (busy) return;
  busy = true; buttons(); message('');
  try { await work(); } catch (error) { message(error.message, true); }
  finally { busy = false; buttons(); }
}

function stopPoll() { if (poll !== null) clearTimeout(poll); poll = null; }

function stopBasketRedirect() {
  if (basketRedirect !== null) clearTimeout(basketRedirect);
  basketRedirect = null;
  redirectTransfer = null;
}

function showPlan(value) {
  stopPoll(); stopBasketRedirect(); plan = value;
  $('ack').checked = false; $('over').checked = false;
  $('review').hidden = false; $('plan').hidden = false;
  $('receipt').replaceChildren();
  const missing = plan.lines.filter((line) => !line.skipped && !line.selected);
  const hasProducts = plan.lines.some((line) => !line.skipped && line.selected);
  $('plan-state').textContent = plan.status === 'ready' ? (missing.length ? 'Ready · some items missing' : 'Ready') : (hasProducts ? 'Needs review' : 'Nothing to add');
  const retailer = retailers.find((r) => r.id === plan.retailer_id);
  const simulated = retailer?.mode === 'mock';
  $('transfer').textContent = simulated ? 'Fill demo basket' : 'Fill basket';
  $('plan-meta').textContent = `${retailer?.name || 'Your shop'} · ${plan.context.fulfillment === 'pickup' ? 'Pickup' : 'Delivery'}${simulated ? ' · Demo' : ''}`;
  $('missing').replaceChildren();
  if (missing.length) {
    const box = el('div', null, 'notice missing');
    box.append(el('strong', 'Not included in the basket'), el('p', hasProducts ? 'These items could not be matched. The rest can still be added.' : 'No matching products found. Nothing will be added.'));
    const list = el('ul');
    for (const line of missing) list.append(el('li', `${line.item.label} · ${line.item.amount.quantity} ${line.item.amount.unit}`));
    box.append(list); $('missing').append(box);
  }
  $('warnings').replaceChildren();
  if (plan.warnings.length) {
    const box = el('div', null, 'notice'); box.append(el('strong', 'Assumptions'));
    for (const warning of plan.warnings) box.append(el('p', warning));
    $('warnings').append(box);
  }
  if (plan.over_budget) $('warnings').append(el('p', `This plan exceeds your ${money({amount_minor: plan.budget_minor, currency: 'SEK'})} budget.`, 'notice'));
  if (!plan.lines.length) $('warnings').append(el('p', 'No grocery items were understood. Describe a dish and how many people it should serve, or enter your grocery list.', 'notice'));
  $('ack-wrap').hidden = !plan.warnings.length; $('over-wrap').hidden = !plan.over_budget;
  $('total').textContent = money(plan.total);
  $('estimate').textContent = `${plan.estimated ? 'Estimated by weight. ' : ''}Delivery excluded. ${simulated ? 'Demo prices.' : 'Confirm prices at checkout.'}`;
  $('lines').replaceChildren(...plan.lines.map(renderLine));
  buttons();
}

function renderLine(line) {
  const node = el('article', null, `line${line.skipped ? ' skipped' : ''}`);
  const top = el('div', null, 'line-top');
  const name = el('div'); name.append(el('h3', line.item.label), el('span', `${line.item.amount.quantity} ${line.item.amount.unit}${line.item.required_traits.length ? ' · ' + line.item.required_traits.join(', ').replaceAll('_', ' ') : ''}`, 'muted'));
  const skip = el('button', line.skipped ? 'Restore' : 'Skip'); skip.type = 'button';
  skip.addEventListener('click', () => action(async () => { showPlan(await patchLine(line, {skipped: !line.skipped})); await history(); }));
  top.append(name, skip); node.append(top);
  if (line.selected && !line.skipped) {
    const selected = line.selected;
    const row = el('div', null, 'line-top');
    row.append(el('p', `${selected.target_quantity} × ${selected.product.name}`, 'product'), el('p', money(selected.total), 'line-price'));
    node.append(row, el('p', `${selected.product.pack.quantity} ${selected.product.pack.unit} / pack · ${money(selected.unit_price)} / pack`, 'muted'));
  } else if (!line.skipped) node.append(el('p', `Not included in the basket. ${line.issue || 'No matching product found.'}`, 'notice'));
  if (line.skipped) return node;
  const details = el('details'); details.append(el('summary', 'Edit or change product'));
  if (line.search_limited) details.append(el('p', `First ${line.search_limit || 500} results checked. Narrow the search for more options.`, 'muted'));
  const form = el('form');
  const grid = el('div', null, 'edit-grid');
  const queryLabel = el('label', 'Search query'), query = el('input'); query.value = line.item.query; query.required = true; query.maxLength = 200; queryLabel.append(query);
  const qtyLabel = el('label', 'Quantity'), qty = el('input'); qty.type = 'number'; qty.value = line.item.amount.quantity; qty.min = 1; qty.max = 1000000; qty.step = 1; qty.required = true; qtyLabel.append(qty);
  const unitLabel = el('label', 'Unit'), unit = el('select');
  for (const [value, label] of [['pack', 'packs'], ['each', 'pieces'], ['g', 'grams'], ['ml', 'millilitres']]) { const option = el('option', label); option.value = value; unit.append(option); }
  unit.value = line.item.amount.unit; unitLabel.append(unit); grid.append(queryLabel, qtyLabel, unitLabel);
  const save = el('button', 'Search again', 'secondary'); save.type = 'submit'; form.append(grid, save);
  form.addEventListener('submit', (event) => { event.preventDefault(); action(async () => {
    const item = {...line.item, query: query.value.trim(), amount: {quantity: Number(qty.value), unit: unit.value}};
    showPlan(await patchLine(line, {item})); await history();
  }); }); details.append(form);
  if (line.observed_at) details.append(el('p', `Prices checked ${new Date(line.observed_at).toLocaleString()}`, 'muted'));
  for (const candidate of line.candidates) {
    const row = el('div', null, 'candidate'), description = el('div');
    description.append(el('strong', candidate.product.name), el('p', `${candidate.target_quantity || '?'} packs · ${money(candidate.total)} · ${candidate.reasons.join(' ')}`, 'muted'));
    const choose = el('button', line.selected?.product.id === candidate.product.id ? 'Selected' : 'Choose'); choose.type = 'button'; choose.disabled = !candidate.eligible || line.selected?.product.id === candidate.product.id;
    choose.addEventListener('click', () => action(async () => { showPlan(await patchLine(line, {product_id: candidate.product.id})); await history(); }));
    row.append(description, choose); details.append(row);
  }
  node.append(details); return node;
}

function patchLine(line, body) { return api(`/v1/shopping-plans/${plan.id}/lines/${line.id}`, {revision: plan.revision, ...body}, 'PATCH'); }

async function history() {
  const result = await api('/v1/shopping-plans', undefined, 'GET');
  $('history').replaceChildren();
  if (!result.plans.length) $('history').textContent = 'No saved lists.';
  for (const saved of result.plans.slice(0, 8)) {
    const button = el('button', `${saved.text.slice(0, 55)}${saved.text.length > 55 ? '…' : ''} · ${money(saved.total)}`); button.type = 'button';
    button.addEventListener('click', () => action(async () => {
      showPlan(await api(`/v1/shopping-plans/${saved.id}`, undefined, 'GET'));
      $('saved-lists').open = false;
      $('review').scrollIntoView({block: 'start', behavior: 'smooth'});
    }));
    $('history').append(button);
  }
}

async function connect() {
  token = $('token').value.trim();
  try {
    const [shops, browserClients, health] = await Promise.all([api('/v1/retailers', undefined, 'GET'), api('/v1/clients', undefined, 'GET'), api('/healthz', undefined, 'GET')]);
    retailers = shops.retailers; clients = browserClients.clients;
    const live = retailers.some((retailer) => retailer.mode === 'browser');
    $('mode-note').textContent = 'Demo products and basket actions are simulated.';
    $('mode-note').hidden = live;
    interpreterMode = health.interpreter;
    if (interpreterMode === 'builtin') {
      $('text').placeholder = '2 litres milk, 12 eggs and 500 g bananas';
      examples.dinner = '500 g pasta\n500 g tomatoes\n1 pack cheese';
    }
    $('reconnect').hidden = true;
    await refreshSessions();
    $('shop-setup').open = !connectedShops().length;
    if (sessionPoll) clearInterval(sessionPoll);
    sessionPoll = setInterval(() => { if (token) refreshSessions().catch(() => { sessions = []; issues = []; updateConnection(); }); }, 5000);
    await history();
  } catch (error) { token = ''; $('reconnect').hidden = false; updateConnection(); throw error; }
}
function updateConnection() {
  const shops = connectedShops(), target = shoppingTarget();
  const options = JSON.stringify(retailers.map(({id, name, mode}) => ({id, name, mode, connected: shops.some((shop) => shop.id === id)})));
  if (options !== shopOptions) {
    shopOptions = options;
    const placeholder = el('option', 'Choose a shop'); placeholder.value = ''; placeholder.disabled = true;
    $('retailer').replaceChildren(placeholder);
    for (const retailer of retailers) {
      const connected = shops.includes(retailer);
      const option = el('option', `${retailer.name}${retailer.mode === 'mock' ? ' · demo' : connected ? '' : ' · not connected'}`); option.value = retailer.id; option.disabled = !connected; $('retailer').append(option);
    }
  }
  $('retailer').value = target ? selectedShop : '';
  const paired = extensionConnected || sessions.length > 0;
  const unconnected = retailers.filter((retailer) => retailer.mode === 'browser' && !shops.includes(retailer));
  $('shop-setup').hidden = !unconnected.length;
  $('add-shop').textContent = shops.length ? 'Connect another shop' : 'Connect a shop';
  $('extension-setup').hidden = paired;
  $('extension-install').hidden = extensionReady;
  $('extension-status').textContent = pairing ? 'Connecting…' : extensionError || (extensionReady ? 'Connect the extension to see your shops.' : 'Install the extension, then reload this page.');
  $('shop-links').hidden = !paired;
  const links = JSON.stringify(unconnected.map(({id, name, basket_url}) => ({id, name, basket_url})));
  if (links !== shopLinks) {
    shopLinks = links;
    $('retailer-links').replaceChildren();
    for (const retailer of unconnected) {
      const link = el('a', retailer.name); link.href = retailer.basket_url; link.target = '_blank'; link.rel = 'noopener noreferrer';
      $('retailer-links').append(link);
    }
  }
  const problems = issues.filter((issue) => unconnected.some((retailer) => retailer.id === issue.retailer_id));
  $('shop-issues').replaceChildren(...problems.map((issue) => el('p', `${retailers.find((retailer) => retailer.id === issue.retailer_id).name} isn't connected. ${issue.message}`, 'muted')));
  $('connection-summary').textContent = shops.length ? 'Shops' : 'Connect shop';
  $('connection-status').textContent = shops.length ? `Connected: ${shops.map((shop) => shop.name).join(', ')}.` : 'No shops connected yet.';
  const shop = retailers.find((retailer) => retailer.id === selectedShop);
  if (!token) {
    $('shop-status').textContent = 'Service disconnected. Reconnect from the Shops menu.';
    $('connection-status').textContent = 'Service disconnected.';
  } else if (target) {
    $('shop-status').textContent = target.retailer.mode === 'browser' && !target.loggedIn ? `Sign in at ${target.retailer.name} before filling your basket.` : '';
  } else {
    $('shop-status').textContent = shop ? `${shop.name} disconnected. Reopen its tab or choose another shop.` : shops.length ? 'Choose a shop to start.' : 'Connect a shop from the Shops menu to start.';
  }
  buttons();
}
async function refreshSessions() {
  const result = await api('/v1/browser-sessions', undefined, 'GET');
  sessions = result.sessions;
  issues = result.issues;
  updateConnection();
  document.dispatchEvent(new CustomEvent('ship-it-extension-probe'));
}
$('retailer').addEventListener('change', () => {
  selectedShop = $('retailer').value;
  updateConnection();
});
$('reconnect').addEventListener('click', () => action(connect));
document.addEventListener('ship-it-extension-ready', (event) => {
  extensionReady = true;
  try { extensionConnected = JSON.parse(event.detail || '{}').connected === true; } catch { extensionConnected = false; }
  updateConnection();
});
document.addEventListener('ship-it-paired', (event) => {
  pairing = false;
  try {
    const result = JSON.parse(event.detail);
    extensionError = result.error || '';
    extensionConnected = !result.error;
    refreshSessions().catch(() => {});
  } catch { extensionError = 'Pairing failed. Reload the page and try again.'; }
  updateConnection();
});
$('pair-extension').addEventListener('click', () => { pairing = true; extensionError = ''; updateConnection(); document.dispatchEvent(new CustomEvent('ship-it-pair')); });
document.dispatchEvent(new CustomEvent('ship-it-extension-probe'));
for (const button of document.querySelectorAll('[data-example]')) button.addEventListener('click', () => { $('text').value = examples[button.dataset.example]; $('text').focus(); });
$('ack').addEventListener('change', buttons); $('over').addEventListener('change', buttons);

$('plan-form').addEventListener('submit', (event) => { event.preventDefault(); action(async () => {
  const target = shoppingTarget();
  if (!token || !target) {
    if (token && connectedShops().length) { $('retailer').focus(); return; }
    const settings = $('connection-settings');
    settings.open = true;
    settings.scrollIntoView({block: 'start', behavior: 'smooth'});
    settings.querySelector('summary').focus();
    if (token) {
      $('shop-setup').open = true;
      if (!extensionReady) $('extension-install').open = true;
    }
    return;
  }
  message(interpreterMode === 'builtin' ? 'Finding products…' : 'Planning ingredients and finding products. Allow about a minute.');
  const {retailer, context} = target;
  const required_traits = Array.from(document.querySelectorAll('.traits input:checked'), (input) => input.value);
  const result = await api('/v1/shopping-plans', {text: $('text').value, retailer_id: retailer.id, context, required_traits, budget_minor: Math.round(Number($('budget').value || 0) * 100)});
  showPlan(result); message(''); await history();
  $('review').scrollIntoView({block: 'start', behavior: 'smooth'});
}); });

$('refresh').addEventListener('click', () => action(async () => { showPlan(await api(`/v1/shopping-plans/${plan.id}/refresh`, {revision: plan.revision})); await history(); }));

function showReceipt(transfer) {
  const simulated = retailers.find((r) => r.id === transfer.retailer_id)?.mode === 'mock';
  const states = {pending: 'Sent to your browser. Keep your shop tab open.', claimed: 'Filling your basket…', completed: 'Your basket is ready to review.', partial: 'Some items need your review.', login_required: 'Sign in at your shop before filling the basket.', uncertain: 'Review these items in your shop basket.', failed: 'The basket could not be updated.'};
  $('receipt').replaceChildren(el('p', `${simulated ? 'Demo: ' : ''}${states[transfer.state] || 'Checking your basket…'}`, 'notice'));
  const labels = {applied: 'Added', unavailable: 'Unavailable', failed: 'Could not add', login_required: 'Sign in at your shop', uncertain: 'Check at your shop'};
  if (transfer.receipt) for (const line of transfer.receipt.lines) {
    const name = plan?.lines.find((item) => item.selected?.product.id === line.product_id)?.selected.product.name || 'Item';
    $('receipt').append(el('p', `${name}: ${labels[line.status] || 'Needs review'}${line.confirmed_quantity ? ` · ${line.confirmed_quantity} packs` : ''}${line.message ? ` · ${line.message}` : ''}`));
  }
  if (!simulated) {
    const link = el('a', 'Open shop basket ↗'); link.href = transfer.basket_url; $('receipt').append(link);
    if (transfer.state === 'completed') {
      $('receipt').append(el('p', 'Opening the shop basket in 3 seconds.', 'muted'));
      if (redirectTransfer !== transfer.id) {
        stopBasketRedirect(); redirectTransfer = transfer.id;
        basketRedirect = setTimeout(() => { basketRedirect = null; window.location.assign(transfer.basket_url); }, 3000);
      }
    } else if (redirectTransfer !== null) stopBasketRedirect();
  }
  if (simulated) stopBasketRedirect();
  $('receipt').append(el('p', simulated ? 'Simulation only.' : 'Review the basket before checkout.', 'muted'));
}

$('transfer').addEventListener('click', () => action(async () => {
  stopBasketRedirect();
  const clientID = planClient();
  if (!clientID) throw new Error('Open your shop and sign in before filling your basket.');
  const scope = `ship-it-transfer:${plan.id}:${plan.revision}:${clientID}`;
  let key = sessionStorage.getItem(scope); if (!key) { key = crypto.randomUUID(); sessionStorage.setItem(scope, key); }
  const result = await api(`/v1/shopping-plans/${plan.id}/transfer`, {revision: plan.revision, client_id: clientID, acknowledge_warnings: $('ack').checked, allow_over_budget: $('over').checked}, 'POST', key);
  showReceipt(result);
  stopPoll();
  const check = async () => {
    try {
      const result = await api(`/v1/basket-transfers/${resultId}`, undefined, 'GET'); showReceipt(result);
      if (['pending', 'claimed'].includes(result.state)) poll = setTimeout(check, 1000);
    } catch (error) { message(error.message, true); }
  };
  const resultId = result.id;
  if (['pending', 'claimed'].includes(result.state)) poll = setTimeout(check, 500);
}));

window.addEventListener('pagehide', () => { stopPoll(); stopBasketRedirect(); if (sessionPoll) clearInterval(sessionPoll); });
let savedPreferences = [];
try { savedPreferences = JSON.parse(localStorage.getItem('ship-it-preferences') || '[]'); } catch { localStorage.removeItem('ship-it-preferences'); }
function updateOptions() {
  const preferences = Array.from(document.querySelectorAll('.traits input:checked'), (input) => input.parentElement.textContent.trim());
  if (Number($('budget').value) > 0) preferences.push(`${$('budget').value} SEK`);
  $('options-summary').textContent = preferences.length ? `Options · ${preferences.join(', ')}` : 'Options';
}
for (const input of document.querySelectorAll('.traits input')) {
  input.checked = Array.isArray(savedPreferences) && savedPreferences.includes(input.value);
  input.addEventListener('change', () => {
    localStorage.setItem('ship-it-preferences', JSON.stringify(Array.from(document.querySelectorAll('.traits input:checked'), (input) => input.value)));
    updateOptions();
  });
}
$('budget').addEventListener('input', updateOptions);
updateOptions();
action(connect);
