'use strict';

const $ = (id) => document.getElementById(id);
const money = (m) => new Intl.NumberFormat('sv-SE', {style: 'currency', currency: m.currency}).format(m.amount_minor / 100);
const el = (tag, text, cls) => { const node = document.createElement(tag); if (text != null) node.textContent = text; if (cls) node.className = cls; return node; };
let token = $('token').value.trim(), plan = null, busy = false, retailers = [], clients = [], poll = null, interpreterMode = '', openLine = '', fill = null;
let browserExtension = {ready: false, connected: false, clientID: '', error: ''};
const shopName = (id) => retailers.find((retailer) => retailer.id === id)?.name || id;
const planRetailer = () => plan ? retailers.find((retailer) => retailer.id === plan.retailer_id) : null;
const chosenBasket = (value = plan) => value.baskets.find((basket) => basket.retailer_id === value.retailer_id);
const entry = (line) => line.baskets[plan.retailer_id];
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
function startProgress(shops) {
  progress = {shops, stage: 'interpret', notes: {}, done: 0, total: 0, started: Date.now()};
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
    ['search', `Search ${progress.shops}`, progress.total ? `${progress.done} of ${progress.total} groceries` : ''],
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
  $('create').disabled = busy || !token || !retailers.length;
  const filling = ['pending', 'claimed'].includes(fill?.state), finished = Boolean(fill) && !filling, completed = fill?.state === 'completed';
  const shop = planRetailer(), fillLabel = !shop ? 'Fill basket' : shop.mode === 'mock' ? `Fill ${shop.name} demo basket` : `Fill ${shop.name} basket`;
  $('refresh').hidden = finished;
  $('refresh').disabled = busy || filling || !plan || !plan.lines.length;
  $('transfer').className = completed ? '' : 'primary';
  $('transfer').textContent = filling ? 'Filling…' : finished ? 'Fill basket again' : fillLabel;
  $('transfer').disabled = busy || filling || !plan || chosenBasket().status !== 'ready' || (plan.warnings.length > 0 && !$('ack').checked) || (chosenBasket().over_budget && !$('over').checked) || !planClient();
  $('filled').hidden = !completed;
  $('new-list').hidden = !finished;
  $('new-list').className = completed ? 'primary' : '';
  $('reconnect').disabled = busy;
}

async function action(work) {
  if (busy) return;
  busy = true; buttons(); message('');
  try { await work(); } catch (error) { message(error.message, true); }
  finally { busy = false; buttons(); }
}

function stopPoll() { if (poll !== null) clearTimeout(poll); poll = null; }

const fillResult = (line) => entry(line).selected && fill?.receipt?.lines.find((result) => result.product_id === entry(line).selected.product.id);
const notAdded = (line) => !line.skipped && Boolean(fillResult(line)) && fillResult(line).status !== 'applied';
const needsAttention = (line) => !line.skipped && (!entry(line).selected?.eligible || notAdded(line));
const fillLabels = {unavailable: 'Unavailable', failed: 'Not added', login_required: 'Sign in', uncertain: 'Check'};
const amount = (item) => `${item.amount.quantity} ${item.amount.unit}${item.required_traits.length ? ' · ' + item.required_traits.join(', ').replaceAll('_', ' ') : ''}`;
const unmarked = (candidate) => candidate.unmet_traits?.length ? `Not marked ${new Intl.ListFormat('en', {type: 'disjunction'}).format(candidate.unmet_traits.map((trait) => trait.replaceAll('_', ' ')))}` : '';
function closest(line) {
  const found = entry(line), best = found.candidates.find((candidate) => candidate.eligible);
  return !found.selected && best?.unmet_traits?.length ? best : null;
}

function showPlan(value) {
  stopPoll();
  if (plan?.id !== value.id) openLine = '';
  plan = value; fill = null;
  $('ack').checked = false; $('over').checked = false;
  $('review').hidden = false; $('checkout').hidden = false;
  $('receipt').replaceChildren();
  const basket = chosenBasket(), missing = plan.lines.filter(needsAttention);
  const hasProducts = plan.lines.some((line) => !line.skipped && entry(line).selected?.eligible);
  $('plan-state').textContent = basket.status === 'ready' ? (missing.length ? 'Ready · some items missing' : 'Ready') : (hasProducts ? 'Needs review' : 'Nothing to add');
  const simulated = planRetailer()?.mode === 'mock';
  renderBaskets();
  $('missing').textContent = !plan.lines.length ? 'No grocery items were understood. Describe a dish and how many people it should serve, or enter your grocery list.'
    : !missing.length ? '' : hasProducts ? `${missing.length === 1 ? '1 item needs' : `${missing.length} items need`} attention and won’t be added until you fix ${missing.length === 1 ? 'it' : 'them'}. ${missing.length === 1 ? 'It is' : 'They are'} at the top of the list.`
    : `No matching products were found at ${shopName(plan.retailer_id)}, so nothing will be added.`;
  $('missing').hidden = !$('missing').textContent;
  $('notes').replaceChildren();
  if (plan.warnings.length) {
    const list = el('ul');
    for (const warning of plan.warnings) list.append(el('li', warning));
    $('notes').append(el('h3', 'Assumptions'), list);
  }
  $('ack-wrap').hidden = !plan.warnings.length; $('over-wrap').hidden = !basket.over_budget;
  if (basket.over_budget) $('over-text').textContent = `Accept going ${money({amount_minor: basket.total.amount_minor - plan.budget_minor, currency: basket.total.currency})} over your ${money({amount_minor: plan.budget_minor, currency: basket.total.currency})} budget.`;
  $('total').textContent = money(basket.total);
  $('estimate').textContent = `${basket.estimated ? 'Estimated by weight. ' : ''}Delivery excluded. ${simulated ? 'Demo prices.' : 'Confirm prices at checkout.'}`;
  renderList();
  updateExtension();
}

function renderBaskets() {
  $('shops').hidden = plan.baskets.length < 2;
  $('baskets').replaceChildren(...plan.baskets.map((basket) => {
    const chosen = basket.retailer_id === plan.retailer_id;
    const card = el('button', null, `basket${chosen ? ' chosen' : ''}`); card.type = 'button';
    card.setAttribute('aria-pressed', String(chosen));
    card.append(el('span', shopName(basket.retailer_id), 'basket-name'), el('strong', money(basket.total), 'basket-total'), el('span', basket.missing ? `${basket.missing} missing` : 'Everything found', 'basket-detail'));
    card.addEventListener('click', () => { if (!chosen) action(async () => { showPlan(await api(`/v1/shopping-plans/${plan.id}`, {revision: plan.revision, retailer_id: basket.retailer_id}, 'PATCH')); await history(); }); });
    return card;
  }));
}

function renderList() {
  const groups = [['Needs attention', plan.lines.filter(needsAttention)], ['Matched', plan.lines.filter((line) => !line.skipped && !needsAttention(line))], ['Skipped', plan.lines.filter((line) => line.skipped)]].filter(([, lines]) => lines.length);
  $('list').className = `list-panel${plan.lines.length ? '' : ' empty'}`;
  $('list-empty').hidden = plan.lines.length > 0;
  $('list-count').textContent = `${plan.lines.length === 1 ? '1 item' : `${plan.lines.length} items`} · ${shopName(plan.retailer_id)}`;
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
  const attention = needsAttention(line), selected = entry(line).selected, result = notAdded(line) && fillResult(line), alternative = closest(line);
  const node = el('details', null, `line${attention ? ' attention' : ''}`);
  node.open = line.id === openLine;
  node.addEventListener('toggle', () => { if (node.open) openLine = line.id; else if (openLine === line.id) openLine = ''; });
  const summary = el('summary'), text = el('span', null, 'line-text');
  const detail = result ? `${selected.target_quantity} × ${selected.product.name} · ${result.message || 'Not added to the basket.'}`
    : alternative ? `Closest: ${alternative.product.name} · ${unmarked(alternative)}`
    : attention ? entry(line).issue || (selected ? 'Choose another product.' : 'No matching product found.')
    : [`${selected.target_quantity} × ${selected.product.name}`, unmarked(selected)].filter(Boolean).join(' · ');
  text.append(el('span', line.item.label, 'line-name'), el('span', `${amount(line.item)} · ${detail}`, 'line-detail'));
  summary.append(text, result ? el('span', fillLabels[result.status] || 'Check', 'line-flag') : attention ? el('span', selected || alternative ? 'Check' : 'Not found', 'line-flag') : el('span', money(selected.total), 'line-price'));
  node.append(summary, renderEditor(line));
  return node;
}

function renderEditor(line) {
  const panel = el('div', null, 'line-edit'), found = entry(line), selected = found.selected;
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
  if (found.search_limited) panel.append(el('p', `First ${found.search_limit || 500} results checked. Narrow the search for more options.`, 'muted'));
  const eligible = found.candidates.filter((candidate) => candidate.eligible), others = found.candidates.filter((candidate) => !candidate.eligible);
  panel.append(...eligible.map((candidate) => renderCandidate(line, candidate)));
  if (others.length) {
    const rest = el('details', null, 'others');
    rest.append(el('summary', `${others.length === 1 ? '1 product doesn’t' : `${others.length} products don’t`} fit`), ...others.map((candidate) => renderCandidate(line, candidate)));
    panel.append(rest);
  }
  const footer = el('div', null, 'line-footer');
  footer.append(el('span', found.observed_at ? `Prices checked ${new Date(found.observed_at).toLocaleString()}` : '', 'muted'));
  const skip = el('button', 'Skip item'); skip.type = 'button';
  skip.addEventListener('click', () => action(async () => { showPlan(await patchLine(line, {skipped: true})); await history(); }));
  footer.append(skip); panel.append(footer);
  return panel;
}

function renderCandidate(line, candidate) {
  const row = el('div', null, 'candidate'), description = el('div');
  const notes = [`${candidate.target_quantity || '?'} × ${candidate.product.pack.quantity} ${candidate.product.pack.unit}`, money(candidate.total), unmarked(candidate), candidate.eligible ? '' : candidate.reasons.join(' ')];
  description.append(el('strong', candidate.product.name), el('p', notes.filter(Boolean).join(' · '), 'muted'));
  const chosen = entry(line).selected?.product.id === candidate.product.id;
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
    const button = el('button', `${saved.text.slice(0, 55)}${saved.text.length > 55 ? '…' : ''} · ${shopName(saved.retailer_id)} ${money(saved.total)}`); button.type = 'button';
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
    updateConnection();
    await history();
  } catch (error) { token = ''; $('reconnect').hidden = false; updateConnection(); throw error; }
}
function updateConnection() {
  $('connection').textContent = token ? '' : 'Service disconnected. Select Reconnect.';
  buttons();
}
function updateExtension() {
  $('extension-setup').hidden = planRetailer()?.mode !== 'browser' || browserExtension.connected;
  $('extension-status').textContent = browserExtension.error || 'The Shop It extension is not running in this browser. Install it, or reload it if it is already installed, then reload this page.';
  $('extension-install').hidden = browserExtension.ready;
  buttons();
}
$('reconnect').addEventListener('click', () => action(connect));
document.addEventListener('shop-it-extension-ready', (event) => {
  let detail = {};
  try { detail = JSON.parse(event.detail || '{}'); } catch {}
  browserExtension = {ready: true, connected: detail.connected === true, clientID: detail.client_id || '', error: detail.error || ''};
  updateExtension();
});
document.dispatchEvent(new CustomEvent('shop-it-extension-probe'));
for (const button of document.querySelectorAll('[data-example]')) button.addEventListener('click', () => { $('text').value = examples[button.dataset.example]; $('text').focus(); });
$('ack').addEventListener('change', buttons); $('over').addEventListener('change', buttons);

$('plan-form').addEventListener('submit', (event) => { event.preventDefault(); action(async () => {
  startProgress(new Intl.ListFormat('en').format(retailers.map((retailer) => retailer.name)));
  try {
    showPlan(await createPlan({text: $('text').value, budget_minor: Math.round(Number($('budget').value || 0) * 100)}, updateProgress));
  } finally { stopProgress(); }
  await history();
  $('review').scrollIntoView({block: 'start', behavior: 'smooth'});
}); });

$('refresh').addEventListener('click', () => action(async () => { showPlan(await api(`/v1/shopping-plans/${plan.id}/refresh`, {revision: plan.revision})); await history(); }));

function showFill(transfer) {
  fill = transfer;
  const retailer = retailers.find((r) => r.id === transfer.retailer_id);
  const simulated = retailer?.mode === 'mock', name = retailer?.name || 'the shop';
  const waiting = !simulated && transfer.state === 'pending' && Date.now() - Date.parse(transfer.created_at) > 10000;
  const progress = {pending: simulated ? 'Filling your basket…' : waiting ? 'Waiting for the Shop It extension. If nothing happens, reload the extension and this page.' : `Opening ${name} in a new tab…`, claimed: `Filling your ${name} basket…`};
  const problems = {expired: 'The Shop It extension did not pick this up. Make sure it is running, then fill the basket again.', partial: 'Some items were not added. They are at the top of the list.', login_required: `Sign in at ${name}, then fill the basket again.`, uncertain: `Some items may be missing from your ${name} basket. They are at the top of the list.`, failed: 'The basket could not be updated.'};
  const text = progress[transfer.state] || problems[transfer.state] || (transfer.state === 'completed' ? '' : 'Checking your basket…');
  $('receipt').replaceChildren(...(text ? [el('p', `${simulated ? 'Demo: ' : ''}${text}`, problems[transfer.state] ? 'callout' : 'muted')] : []));
  $('filled').textContent = simulated ? '✓ Demo basket filled' : `✓ Basket filled at ${name}`;
  renderList();
  buttons();
}

function newList() {
  stopPoll();
  plan = null; fill = null; openLine = '';
  $('review').hidden = true; $('checkout').hidden = true;
  $('lines').replaceChildren(); $('list-count').textContent = '';
  $('list').className = 'list-panel empty'; $('list-empty').hidden = false;
  $('text').value = ''; message('');
  buttons();
  window.scrollTo({top: 0, behavior: 'smooth'});
  $('text').focus({preventScroll: true});
}
$('new-list').addEventListener('click', newList);

$('transfer').addEventListener('click', () => action(async () => {
  const clientID = planClient();
  if (!clientID) throw new Error('The Shop It extension is not running in this browser.');
  const scope = `shop-it-transfer:${plan.id}:${plan.revision}:${clientID}`;
  let key = sessionStorage.getItem(scope); if (!key) { key = crypto.randomUUID(); sessionStorage.setItem(scope, key); }
  const result = await api(`/v1/shopping-plans/${plan.id}/transfer`, {revision: plan.revision, client_id: clientID, acknowledge_warnings: $('ack').checked, allow_over_budget: $('over').checked}, 'POST', key);
  document.dispatchEvent(new CustomEvent('shop-it-fill'));
  const follow = (transfer) => {
    showFill(transfer);
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
function updateOptions() {
  $('options-summary').textContent = Number($('budget').value) > 0 ? `Options · ${$('budget').value} SEK` : 'Options';
}
$('budget').addEventListener('input', updateOptions);
updateOptions();
action(connect);
