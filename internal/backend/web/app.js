'use strict';

const $ = (id) => document.getElementById(id);
const money = (m) => new Intl.NumberFormat('sv-SE', {style: 'currency', currency: m.currency}).format(m.amount_minor / 100);
const el = (tag, text, cls) => { const node = document.createElement(tag); if (text != null) node.textContent = text; if (cls) node.className = cls; return node; };
let token = $('token').value.trim(), plan = null, busy = false, retailers = [], clients = [], poll = null, interpreterMode = '', selectedShop = '';
let browserExtension = {ready: false, connected: false, clientID: '', error: ''};
const shoppingTarget = () => retailers.find((retailer) => retailer.id === selectedShop) || null;
const planRetailer = () => plan ? retailers.find((retailer) => retailer.id === plan.retailer_id) : null;
function planClient() {
  const retailer = planRetailer();
  if (!retailer) return null;
  return retailer.mode === 'browser' ? (browserExtension.connected && browserExtension.clientID) || null : clients[0]?.id;
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
    const messages = {400: 'Check your input, quantity and selected product.', 401: 'The connection expired. Reload this page to reconnect.', 409: 'The plan changed or still needs review. Reopen it, resolve all lines, and review the warnings.', 502: 'The AI or the shop did not respond. Check your AI login and try again.', 504: 'Planning ingredients or searching the shop took too long. Please try again.'};
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
  $('retailer').disabled = busy || !retailers.length;
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
  $('review').hidden = false; $('plan').hidden = false;
  $('receipt').replaceChildren();
  const missing = plan.lines.filter((line) => !line.skipped && !line.selected);
  const hasProducts = plan.lines.some((line) => !line.skipped && line.selected);
  $('plan-state').textContent = plan.status === 'ready' ? (missing.length ? 'Ready · some items missing' : 'Ready') : (hasProducts ? 'Needs review' : 'Nothing to add');
  const retailer = retailers.find((r) => r.id === plan.retailer_id);
  const simulated = retailer?.mode === 'mock';
  $('transfer').textContent = simulated ? 'Fill demo basket' : 'Fill basket';
  $('plan-meta').textContent = `${retailer?.name || 'Your shop'}${plan.context.fulfillment ? ` · ${plan.context.fulfillment === 'pickup' ? 'Pickup' : 'Delivery'}` : ''}${simulated ? ' · Demo' : ''}`;
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
  updateExtension();
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
    showShops();
    await history();
  } catch (error) { token = ''; $('reconnect').hidden = false; showShops(); throw error; }
}
function showShops() {
  const placeholder = el('option', 'Choose a shop'); placeholder.value = ''; placeholder.disabled = true;
  $('retailer').replaceChildren(placeholder, ...retailers.map((retailer) => { const option = el('option', `${retailer.name}${retailer.mode === 'mock' ? ' · demo' : ''}`); option.value = retailer.id; return option; }));
  $('retailer').value = selectedShop;
  updateShop();
}
function updateShop() {
  $('shop-status').textContent = !token ? 'Service disconnected. Select Reconnect.' : shoppingTarget() ? '' : 'Choose a shop to start.';
  buttons();
}
function updateExtension() {
  $('extension-setup').hidden = planRetailer()?.mode !== 'browser' || browserExtension.connected;
  $('extension-status').textContent = browserExtension.error || 'The Ship It extension is not running in this browser. Install it, or reload it if it is already installed, then reload this page.';
  $('extension-install').hidden = browserExtension.ready;
  buttons();
}
$('retailer').addEventListener('change', () => {
  selectedShop = $('retailer').value;
  updateShop();
});
$('reconnect').addEventListener('click', () => action(connect));
document.addEventListener('ship-it-extension-ready', (event) => {
  let detail = {};
  try { detail = JSON.parse(event.detail || '{}'); } catch {}
  browserExtension = {ready: true, connected: detail.connected === true, clientID: detail.client_id || '', error: detail.error || ''};
  updateExtension();
});
document.dispatchEvent(new CustomEvent('ship-it-extension-probe'));
for (const button of document.querySelectorAll('[data-example]')) button.addEventListener('click', () => { $('text').value = examples[button.dataset.example]; $('text').focus(); });
$('ack').addEventListener('change', buttons); $('over').addEventListener('change', buttons);

$('plan-form').addEventListener('submit', (event) => { event.preventDefault(); action(async () => {
  const retailer = shoppingTarget();
  if (!token || !retailer) { $('retailer').focus(); return; }
  message(interpreterMode === 'builtin' ? 'Finding products…' : 'Planning ingredients and finding products. Allow about a minute.');
  const required_traits = Array.from(document.querySelectorAll('.traits input:checked'), (input) => input.value);
  const result = await api('/v1/shopping-plans', {text: $('text').value, retailer_id: retailer.id, context: retailer.context, required_traits, budget_minor: Math.round(Number($('budget').value || 0) * 100)});
  showPlan(result); message(''); await history();
  $('review').scrollIntoView({block: 'start', behavior: 'smooth'});
}); });

$('refresh').addEventListener('click', () => action(async () => { showPlan(await api(`/v1/shopping-plans/${plan.id}/refresh`, {revision: plan.revision})); await history(); }));

function showReceipt(transfer) {
  const retailer = retailers.find((r) => r.id === transfer.retailer_id);
  const simulated = retailer?.mode === 'mock', name = retailer?.name || 'the shop';
  const waiting = !simulated && transfer.state === 'pending' && Date.now() - Date.parse(transfer.created_at) > 10000;
  const states = {pending: simulated ? 'Filling your basket…' : waiting ? 'Waiting for the Ship It extension. If nothing happens, reload the extension and this page.' : `Opening ${name} in a new tab…`, expired: 'The Ship It extension did not pick this up. Make sure it is running, then fill the basket again.', claimed: `Filling your ${name} basket…`, completed: simulated ? 'Your basket is ready to review.' : `Your basket is ready in the ${name} tab.`, partial: `Some items need your review in the ${name} tab.`, login_required: `Sign in at ${name}, then fill the basket again.`, uncertain: `Check these items in your ${name} basket.`, failed: 'The basket could not be updated.'};
  $('receipt').replaceChildren(el('p', `${simulated ? 'Demo: ' : ''}${states[transfer.state] || 'Checking your basket…'}`, 'notice'));
  const labels = {applied: 'Added', unavailable: 'Unavailable', failed: 'Could not add', login_required: 'Sign in at your shop', uncertain: 'Check at your shop'};
  if (transfer.receipt) for (const line of transfer.receipt.lines) {
    const product = plan?.lines.find((item) => item.selected?.product.id === line.product_id)?.selected.product.name || 'Item';
    $('receipt').append(el('p', `${product}: ${labels[line.status] || 'Needs review'}${line.confirmed_quantity ? ` · ${line.confirmed_quantity} packs` : ''}${line.message ? ` · ${line.message}` : ''}`));
  }
  if (!simulated) {
    const link = el('a', `Open ${name} basket ↗`); link.href = transfer.basket_url; link.target = '_blank'; link.rel = 'noopener noreferrer';
    $('receipt').append(link);
  }
  $('receipt').append(el('p', simulated ? 'Simulation only.' : 'Review the basket and check out at the shop.', 'muted'));
}

$('transfer').addEventListener('click', () => action(async () => {
  const clientID = planClient();
  if (!clientID) throw new Error('The Ship It extension is not running in this browser.');
  const scope = `ship-it-transfer:${plan.id}:${plan.revision}:${clientID}`;
  let key = sessionStorage.getItem(scope); if (!key) { key = crypto.randomUUID(); sessionStorage.setItem(scope, key); }
  const result = await api(`/v1/shopping-plans/${plan.id}/transfer`, {revision: plan.revision, client_id: clientID, acknowledge_warnings: $('ack').checked, allow_over_budget: $('over').checked}, 'POST', key);
  document.dispatchEvent(new CustomEvent('ship-it-fill'));
  const follow = (transfer) => {
    showReceipt(transfer);
    if (['pending', 'claimed'].includes(transfer.state)) poll = setTimeout(check, 1000);
    else sessionStorage.removeItem(scope);
  };
  const check = async () => {
    try { follow(await api(`/v1/basket-transfers/${result.id}`, undefined, 'GET')); } catch (error) { message(error.message, true); }
  };
  stopPoll();
  follow(result);
}));

window.addEventListener('pagehide', stopPoll);
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
