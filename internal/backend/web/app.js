'use strict';

const $ = (id) => document.getElementById(id);
const money = (m) => new Intl.NumberFormat('sv-SE', {style: 'currency', currency: m.currency}).format(m.amount_minor / 100);
const el = (tag, text, cls) => { const node = document.createElement(tag); if (text != null) node.textContent = text; if (cls) node.className = cls; return node; };
let token = $('token').value.trim(), plan = null, busy = false, retailers = [], clients = [], poll = null, interpreterMode = '', selectedShop = '', openLine = '';
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

const failures = {400: 'Check your input, quantity and selected product.', 401: 'The connection expired. Reload this page to reconnect.', 409: 'The plan changed or still needs review. Reopen it, resolve all lines, and review the warnings.', 502: 'The AI or the shop did not respond. Check your AI login and try again.', 504: 'Planning ingredients or searching the shop took too long. Please try again.'};
const failure = (status, error) => new Error(failures[status] || error || 'The request failed.');

function request(path, body, method, key, accept = 'application/json') {
  const headers = {Authorization: `Bearer ${token}`, Accept: accept};
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  if (key) headers['Idempotency-Key'] = key;
  return fetch(path, {method, headers, body: body === undefined ? undefined : JSON.stringify(body)});
}

async function api(path, body, method = 'POST', key) {
  const response = await request(path, body, method, key);
  const result = await response.json();
  if (!response.ok) throw failure(response.status, result.error);
  return result;
}

async function createPlan(body, onProgress) {
  const response = await request('/v1/shopping-plans', body, 'POST', undefined, 'application/x-ndjson');
  if (!response.ok) throw failure(response.status, (await response.json()).error);
  const reader = response.body.getReader(), decoder = new TextDecoder();
  let buffered = '';
  for (;;) {
    const {done, value} = await reader.read();
    buffered += decoder.decode(value, {stream: !done});
    const lines = buffered.split('\n');
    buffered = lines.pop();
    for (const line of lines.filter((line) => line.trim())) {
      const event = JSON.parse(line);
      if (event.plan) return event.plan;
      if (event.error) throw failure(event.status, event.error);
      if (event.progress) onProgress(event.progress);
    }
    if (done) throw new Error('The connection to the service was lost. Please try again.');
  }
}

let progress = null, progressTimer = null;
function startProgress(shop) {
  progress = {shop, stage: 'interpret', notes: {}, done: 0, total: 0, started: Date.now()};
  $('progress').hidden = false;
  renderProgress();
  $('progress').scrollIntoView({block: 'nearest', behavior: 'smooth'});
  progressTimer = setInterval(renderProgressTime, 1000);
}
function updateProgress(event) {
  progress.stage = event.stage;
  if (event.note) progress.notes[event.stage] = event.note;
  if (event.stage === 'search') { progress.done = Math.max(event.done || 0, progress.done); progress.total = event.total || 0; }
  renderProgress();
}
function stopProgress() {
  clearInterval(progressTimer);
  progress = null; progressTimer = null;
  $('progress').hidden = true;
}
function renderProgressTime() {
  const seconds = Math.floor((Date.now() - progress.started) / 1000);
  $('progress-time').textContent = `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`;
}
function renderProgress() {
  const builtin = interpreterMode === 'builtin';
  const steps = [
    ['interpret', builtin ? 'Read your list' : 'Plan groceries', builtin ? '' : 'Working out what to buy…'],
    ['search', `Search ${progress.shop}`, progress.total ? `${progress.done} of ${progress.total} groceries` : ''],
  ];
  if (!builtin) steps.push(['match', 'Check product matches', 'Making sure each product fits its ingredient…']);
  const current = steps.findIndex(([stage]) => stage === progress.stage);
  $('progress-steps').replaceChildren(...steps.map(([stage, name, detail], index) => {
    const state = index < current ? 'done' : index === current ? 'active' : 'pending';
    const step = el('li', null, `step ${state}`);
    step.append(el('span', null, 'step-mark'), el('span', name, 'step-name'));
    if (state === 'active') step.append(el('span', progress.notes[stage] || detail, 'step-detail'));
    if (state === 'done' && stage === 'interpret' && progress.total) step.append(el('span', `${progress.total} groceries`, 'step-detail'));
    return step;
  }));
  renderProgressTime();
}

function message(text, error = false) {
  $('message').replaceChildren();
  if (text) $('message').append(el('p', text, `notice${error ? ' error' : ''}`));
}

function buttons() {
  $('create').disabled = busy || !token || !shoppingTarget();
  $('retailer').disabled = busy || !retailers.length;
  $('refresh').disabled = busy || !plan || !plan.lines.length;
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

const needsAttention = (line) => !line.skipped && !line.selected?.eligible;
const amount = (item) => `${item.amount.quantity} ${item.amount.unit}${item.required_traits.length ? ' · ' + item.required_traits.join(', ').replaceAll('_', ' ') : ''}`;

function showPlan(value) {
  stopPoll();
  if (plan?.id !== value.id) openLine = '';
  plan = value;
  $('ack').checked = false; $('over').checked = false;
  $('review').hidden = false; $('checkout').hidden = false;
  $('receipt').replaceChildren();
  const missing = plan.lines.filter(needsAttention);
  const hasProducts = plan.lines.some((line) => !line.skipped && line.selected?.eligible);
  $('plan-state').textContent = plan.status === 'ready' ? (missing.length ? 'Ready · some items missing' : 'Ready') : (hasProducts ? 'Needs review' : 'Nothing to add');
  const retailer = retailers.find((r) => r.id === plan.retailer_id);
  const simulated = retailer?.mode === 'mock';
  $('transfer').textContent = simulated ? 'Fill demo basket' : 'Fill basket';
  $('plan-meta').textContent = `${retailer?.name || 'Your shop'}${plan.context.fulfillment ? ` · ${plan.context.fulfillment === 'pickup' ? 'Pickup' : 'Delivery'}` : ''}${simulated ? ' · Demo' : ''}`;
  $('missing').textContent = !plan.lines.length ? 'No grocery items were understood. Describe a dish and how many people it should serve, or enter your grocery list.'
    : !missing.length ? '' : hasProducts ? `${missing.length === 1 ? '1 item needs' : `${missing.length} items need`} attention and won’t be added until you fix ${missing.length === 1 ? 'it' : 'them'}. ${missing.length === 1 ? 'It is' : 'They are'} at the top of the list.`
    : 'No matching products were found, so nothing will be added.';
  $('missing').hidden = !$('missing').textContent;
  $('notes').replaceChildren();
  if (plan.warnings.length) {
    const list = el('ul');
    for (const warning of plan.warnings) list.append(el('li', warning));
    $('notes').append(el('h3', 'Assumptions'), list);
  }
  $('ack-wrap').hidden = !plan.warnings.length; $('over-wrap').hidden = !plan.over_budget;
  if (plan.over_budget) $('over-text').textContent = `Accept going ${money({amount_minor: plan.total.amount_minor - plan.budget_minor, currency: plan.total.currency})} over your ${money({amount_minor: plan.budget_minor, currency: plan.total.currency})} budget.`;
  $('total').textContent = money(plan.total);
  $('estimate').textContent = `${plan.estimated ? 'Estimated by weight. ' : ''}Delivery excluded. ${simulated ? 'Demo prices.' : 'Confirm prices at checkout.'}`;
  renderList();
  updateExtension();
}

function renderList() {
  const groups = [['Needs attention', plan.lines.filter(needsAttention)], ['Matched', plan.lines.filter((line) => !line.skipped && !needsAttention(line))], ['Skipped', plan.lines.filter((line) => line.skipped)]].filter(([, lines]) => lines.length);
  $('list').className = `list-panel${plan.lines.length ? '' : ' empty'}`;
  $('list-empty').hidden = plan.lines.length > 0;
  $('list-count').textContent = plan.lines.length === 1 ? '1 item' : `${plan.lines.length} items`;
  $('lines').replaceChildren(...groups.flatMap(([title, lines]) => [...(groups.length > 1 ? [el('h3', `${title} · ${lines.length}`, 'group')] : []), ...lines.map(renderLine)]));
}

function renderLine(line) {
  if (line.skipped) {
    const node = el('div', null, 'line skipped'), text = el('span', null, 'line-text');
    text.append(el('span', line.item.label, 'line-name'), el('span', amount(line.item), 'line-detail'));
    const restore = el('button', 'Restore'); restore.type = 'button';
    restore.addEventListener('click', () => action(async () => { showPlan(await patchLine(line, {skipped: false})); await history(); }));
    node.append(text, restore);
    return node;
  }
  const attention = needsAttention(line), selected = line.selected;
  const node = el('details', null, `line${attention ? ' attention' : ''}`);
  node.open = line.id === openLine;
  node.addEventListener('toggle', () => { if (node.open) openLine = line.id; else if (openLine === line.id) openLine = ''; });
  const summary = el('summary'), text = el('span', null, 'line-text');
  text.append(el('span', line.item.label, 'line-name'), el('span', attention ? `${amount(line.item)} · ${line.issue || (selected ? 'Choose another product.' : 'No matching product found.')}` : `${amount(line.item)} · ${selected.target_quantity} × ${selected.product.name}`, 'line-detail'));
  summary.append(text, attention ? el('span', selected ? 'Check' : 'Not found', 'line-flag') : el('span', money(selected.total), 'line-price'));
  node.append(summary, renderEditor(line));
  return node;
}

function renderEditor(line) {
  const panel = el('div', null, 'line-edit'), selected = line.selected;
  if (selected) panel.append(el('p', `${selected.product.pack.quantity} ${selected.product.pack.unit} / pack · ${money(selected.unit_price)} / pack`, 'muted'));
  const form = el('form', null, 'edit-form');
  const queryLabel = el('label', 'Search'), query = el('input'); query.value = line.item.query; query.required = true; query.maxLength = 200; queryLabel.append(query);
  const qtyLabel = el('label', 'Quantity'), qty = el('input'); qty.type = 'number'; qty.value = line.item.amount.quantity; qty.min = 1; qty.max = 1000000; qty.step = 1; qty.required = true; qtyLabel.append(qty);
  const unitLabel = el('label', 'Unit'), unit = el('select');
  for (const [value, label] of [['pack', 'packs'], ['each', 'pieces'], ['g', 'grams'], ['ml', 'millilitres']]) { const option = el('option', label); option.value = value; unit.append(option); }
  unit.value = line.item.amount.unit; unitLabel.append(unit);
  const save = el('button', 'Search again'); save.type = 'submit';
  form.append(queryLabel, qtyLabel, unitLabel, save);
  form.addEventListener('submit', (event) => { event.preventDefault(); action(async () => {
    const item = {...line.item, query: query.value.trim(), amount: {quantity: Number(qty.value), unit: unit.value}};
    showPlan(await patchLine(line, {item})); await history();
  }); });
  panel.append(form);
  if (line.search_limited) panel.append(el('p', `First ${line.search_limit || 500} results checked. Narrow the search for more options.`, 'muted'));
  const eligible = line.candidates.filter((candidate) => candidate.eligible), others = line.candidates.filter((candidate) => !candidate.eligible);
  panel.append(...eligible.map((candidate) => renderCandidate(line, candidate)));
  if (others.length) {
    const rest = el('details', null, 'others');
    rest.append(el('summary', `${others.length === 1 ? '1 product doesn’t' : `${others.length} products don’t`} fit`), ...others.map((candidate) => renderCandidate(line, candidate)));
    panel.append(rest);
  }
  const footer = el('div', null, 'line-footer');
  footer.append(el('span', line.observed_at ? `Prices checked ${new Date(line.observed_at).toLocaleString()}` : '', 'muted'));
  const skip = el('button', 'Skip item'); skip.type = 'button';
  skip.addEventListener('click', () => action(async () => { showPlan(await patchLine(line, {skipped: true})); await history(); }));
  footer.append(skip); panel.append(footer);
  return panel;
}

function renderCandidate(line, candidate) {
  const row = el('div', null, 'candidate'), description = el('div');
  description.append(el('strong', candidate.product.name), el('p', `${candidate.target_quantity || '?'} × ${candidate.product.pack.quantity} ${candidate.product.pack.unit} · ${money(candidate.total)}${candidate.eligible ? '' : ` · ${candidate.reasons.join(' ')}`}`, 'muted'));
  const chosen = line.selected?.product.id === candidate.product.id;
  const choose = el('button', chosen ? 'Selected' : 'Choose'); choose.type = 'button'; choose.disabled = !candidate.eligible || chosen;
  choose.addEventListener('click', () => action(async () => { showPlan(await patchLine(line, {product_id: candidate.product.id})); await history(); }));
  row.append(description, choose);
  return row;
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
  const required_traits = Array.from(document.querySelectorAll('.traits input:checked'), (input) => input.value);
  startProgress(retailer.name);
  try {
    showPlan(await createPlan({text: $('text').value, retailer_id: retailer.id, context: retailer.context, required_traits, budget_minor: Math.round(Number($('budget').value || 0) * 100)}, updateProgress));
  } finally { stopProgress(); }
  await history();
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
