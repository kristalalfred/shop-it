'use strict';

const $ = (id) => document.getElementById(id);
const money = (m) => new Intl.NumberFormat('sv-SE', {style: 'currency', currency: m.currency}).format(m.amount_minor / 100);
const el = (tag, text, cls) => { const node = document.createElement(tag); if (text != null) node.textContent = text; if (cls) node.className = cls; return node; };
let token = $('token').value.trim(), plan = null, busy = false, retailers = [], clients = [], poll = null, sessions = [], sessionPoll = null, extensionReady = false;
let preferredShop = localStorage.getItem('ship-it-shop') || '';
const contextEqual = (a, b) => ['store_id', 'postal_code', 'fulfillment', 'delivery_slot_id', 'session_id'].every((key) => (a[key] || '') === (b[key] || '')) && JSON.stringify(a.memberships || []) === JSON.stringify(b.memberships || []);
function shoppingTarget() {
  const shops = preferredShop ? retailers.filter((retailer) => retailer.id === preferredShop) : retailers;
  const available = shops.flatMap((retailer) => retailer.mode === 'browser' ? sessions.filter((session) => session.retailer_id === retailer.id).map((session) => ({retailer, context: session.context, clientID: session.client_id, loggedIn: session.logged_in})) : [{retailer, context: {store_id: `${retailer.id}-demo`, postal_code: '11122', fulfillment: 'delivery', memberships: retailer.mode === 'mock' ? [`${retailer.id}-demo-member`] : []}, clientID: clients[0]?.id}]);
  return available.find((target) => target.loggedIn) || available[0] || null;
}
function planClient() {
  if (!plan) return null;
  const retailer = retailers.find((retailer) => retailer.id === plan.retailer_id);
  return retailer?.mode === 'browser' ? sessions.find((session) => session.retailer_id === plan.retailer_id && session.logged_in && contextEqual(session.context, plan.context))?.client_id : clients[0]?.id;
}
const examples = {
  breakfast: 'We need 2 litres of organic milk, 12 eggs and 500 g bananas.',
  dinner: '500 g pasta\n500 g tomatoes\n1 pack cheese',
  diet: '2 litres lactose-free milk\n1 pack gluten-free bread\n500 g bananas',
};

async function api(path, body, method = 'POST', key) {
  const headers = {Authorization: `Bearer ${token}`};
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  if (key) headers['Idempotency-Key'] = key;
  const response = await fetch(path, {method, headers, body: body === undefined ? undefined : JSON.stringify(body)});
  const result = await response.json();
  if (!response.ok) {
    const messages = {400: 'Check your input, quantity and selected product.', 401: 'The connection expired. Reload this page to reconnect.', 409: 'The plan changed or still needs review. Reopen it, resolve all lines, and review the warnings.', 502: 'We could not reach your shop. Keep its browser tab open and try again.', 504: 'The interpreter or catalog took too long. Please try again.'};
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
  $('pair-extension').disabled = busy || !token || !extensionReady;
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

function showPlan(value) {
  stopPoll(); plan = value;
  $('ack').checked = false; $('over').checked = false;
  $('empty').hidden = true; $('plan').hidden = false;
  $('receipt').replaceChildren();
  $('plan-state').textContent = plan.status === 'ready' ? 'Ready to review' : 'Needs your input';
  const retailer = retailers.find((r) => r.id === plan.retailer_id);
  const simulated = retailer?.mode === 'mock';
  $('transfer').textContent = simulated ? 'Send to demo basket ↗' : 'Fill basket ↗';
  $('plan-meta').textContent = `${retailer?.name || 'Your shop'} · ${plan.context.fulfillment === 'pickup' ? 'Pickup' : 'Delivery'}${simulated ? ' · Demo' : ''}`;
  $('warnings').replaceChildren();
  if (plan.warnings.length) {
    const box = el('div', null, 'notice'); box.append(el('strong', 'Check these assumptions'));
    for (const warning of plan.warnings) box.append(el('p', warning));
    $('warnings').append(box);
  }
  if (plan.over_budget) $('warnings').append(el('p', `This plan exceeds your ${money({amount_minor: plan.budget_minor, currency: 'SEK'})} budget.`, 'notice'));
  if (!plan.lines.length) $('warnings').append(el('p', 'No grocery items were understood. Try a list with one item per line.', 'notice'));
  $('ack-wrap').hidden = !plan.warnings.length; $('over-wrap').hidden = !plan.over_budget;
  $('total').textContent = money(plan.total);
  $('estimate').textContent = `${plan.estimated ? 'Estimated weight-based price. ' : ''}Excludes delivery fees. ${simulated ? 'Synthetic demo prices.' : 'Prices may change before checkout.'}`;
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
    node.append(row, el('p', `${selected.product.brand} · ${selected.product.pack.quantity} ${selected.product.pack.unit} per pack · ${money(selected.unit_price)} per pack`, 'muted'));
  } else if (!line.skipped) node.append(el('p', line.issue || 'Choose a product or skip this line.', 'notice'));
  if (line.skipped) return node;
  const details = el('details'); details.append(el('summary', 'Edit request or choose another product'));
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
  if (line.observed_at) details.append(el('p', `Catalog observed ${new Date(line.observed_at).toLocaleString()}`, 'muted'));
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
  if (!result.plans.length) $('history').textContent = 'Your plans will appear here.';
  for (const saved of result.plans.slice(0, 8)) {
    const button = el('button', `${saved.text.slice(0, 55)}${saved.text.length > 55 ? '…' : ''} · ${money(saved.total)}`); button.type = 'button';
    button.addEventListener('click', () => action(async () => showPlan(await api(`/v1/shopping-plans/${saved.id}`, undefined, 'GET'))));
    $('history').append(button);
  }
}

async function connect() {
  token = $('token').value.trim();
  try {
    const [shops, browserClients, health] = await Promise.all([api('/v1/retailers', undefined, 'GET'), api('/v1/clients', undefined, 'GET'), api('/healthz', undefined, 'GET')]);
    retailers = shops.retailers; clients = browserClients.clients;
    if (!retailers.some((retailer) => retailer.id === preferredShop)) preferredShop = '';
    $('retailer').replaceChildren();
    const automatic = el('option', 'Automatic'); automatic.value = ''; $('retailer').append(automatic);
    for (const retailer of retailers) { const option = el('option', `${retailer.name}${retailer.mode === 'mock' ? ' · demo' : ''}`); option.value = retailer.id; $('retailer').append(option); }
    $('retailer').value = preferredShop;
    const live = retailers.some((retailer) => retailer.mode === 'browser');
    $('connection-status').textContent = live ? 'Ready to connect your browser.' : 'Demo shopping is ready.';
    $('mode-note').textContent = live ? 'You review your basket at the shop before checkout.' : 'Demo products and basket actions are simulated.';
    if (health.interpreter === 'builtin') $('text').placeholder = '2 litres milk, 12 eggs and 500 g bananas';
    $('extension-setup').hidden = !live;
    $('reconnect').hidden = true;
    await refreshSessions();
    if (sessionPoll) clearInterval(sessionPoll);
    sessionPoll = setInterval(() => { if (token) refreshSessions().catch(() => { sessions = []; updateConnection(); }); }, 5000);
    await history();
  } catch (error) { token = ''; $('reconnect').hidden = false; throw error; }
}
function updateConnection() {
  const target = shoppingTarget();
  if (target) {
    $('shop-status').textContent = `Shopping at ${target.retailer.name}${target.retailer.mode === 'mock' ? ' · Demo' : ''}.`;
    if (target.retailer.mode === 'browser') {
      $('connection-status').textContent = `${target.retailer.name} connected${target.loggedIn ? '.' : '. Sign in at the shop before filling your basket.'}`;
    }
  } else {
    const shop = retailers.find((retailer) => retailer.id === preferredShop);
    const instruction = extensionReady ? `Open ${shop?.name || 'Mathem, Willys or Hemköp'} in your connected browser to start shopping.` : 'Connect your browser to start shopping.';
    $('shop-status').textContent = instruction;
    $('connection-status').textContent = instruction;
  }
  buttons();
}
async function refreshSessions() {
  const result = await api('/v1/browser-sessions', undefined, 'GET');
  sessions = result.sessions;
  updateConnection();
}
$('retailer').addEventListener('change', () => {
  preferredShop = $('retailer').value;
  localStorage.setItem('ship-it-shop', preferredShop);
  updateConnection();
});
$('reconnect').addEventListener('click', () => action(connect));
document.addEventListener('ship-it-extension-ready', () => { extensionReady = true; $('extension-status').textContent = 'Extension installed. Connect it to start shopping.'; updateConnection(); });
document.addEventListener('ship-it-paired', (event) => {
  try {
    const result = JSON.parse(event.detail);
    $('extension-status').textContent = result.error || 'Browser connected. Open your shop to start shopping.';
    refreshSessions().catch(() => {});
  } catch { $('extension-status').textContent = 'Pairing failed. Reload the page and try again.'; }
});
$('pair-extension').addEventListener('click', () => { $('extension-status').textContent = 'Connecting…'; document.dispatchEvent(new CustomEvent('ship-it-pair')); });
document.dispatchEvent(new CustomEvent('ship-it-extension-probe'));
for (const button of document.querySelectorAll('[data-example]')) button.addEventListener('click', () => { $('text').value = examples[button.dataset.example]; $('text').focus(); });
$('ack').addEventListener('change', buttons); $('over').addEventListener('change', buttons);

$('plan-form').addEventListener('submit', (event) => { event.preventDefault(); action(async () => {
  message('Finding products and checking pack sizes…');
  const target = shoppingTarget();
  if (!target) throw new Error('Connect your browser and open your shop first.');
  const {retailer, context} = target;
  const required_traits = Array.from(document.querySelectorAll('.traits input:checked'), (input) => input.value);
  const result = await api('/v1/shopping-plans', {text: $('text').value, retailer_id: retailer.id, context, required_traits, budget_minor: Math.round(Number($('budget').value || 0) * 100)});
  showPlan(result); message(''); await history();
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
  if (!simulated) { const link = el('a', 'Review basket at the store ↗'); link.href = transfer.basket_url; link.target = '_blank'; link.rel = 'noopener noreferrer'; $('receipt').append(link); }
  $('receipt').append(el('p', simulated ? 'This is a simulated result. No real retailer basket was changed.' : 'Review your basket at the shop before checkout.', 'muted'));
}

$('transfer').addEventListener('click', () => action(async () => {
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

window.addEventListener('pagehide', () => { stopPoll(); if (sessionPoll) clearInterval(sessionPoll); });
let savedPreferences = [];
try { savedPreferences = JSON.parse(localStorage.getItem('ship-it-preferences') || '[]'); } catch { localStorage.removeItem('ship-it-preferences'); }
for (const input of document.querySelectorAll('.traits input')) {
  input.checked = Array.isArray(savedPreferences) && savedPreferences.includes(input.value);
  input.addEventListener('change', () => localStorage.setItem('ship-it-preferences', JSON.stringify(Array.from(document.querySelectorAll('.traits input:checked'), (input) => input.value))));
}
action(connect);
