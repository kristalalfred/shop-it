'use strict';

const $ = (id) => document.getElementById(id);
const money = (m) => new Intl.NumberFormat('sv-SE', {style: 'currency', currency: m.currency}).format(m.amount_minor / 100);
const el = (tag, text, cls) => { const node = document.createElement(tag); if (text != null) node.textContent = text; if (cls) node.className = cls; return node; };
let token = '', plan = null, busy = false, retailers = [], poll = null, sessions = [], sessionPoll = null, extensionReady = false;
const liveRetailer = () => retailers.find((r) => r.id === $('retailer').value)?.mode === 'browser';
const selectedSession = () => sessions.find((session) => `${session.client_id}:${session.context.session_id}` === $('session').value);
const contextEqual = (a, b) => ['store_id', 'postal_code', 'fulfillment', 'delivery_slot_id', 'session_id'].every((key) => (a[key] || '') === (b[key] || '')) && JSON.stringify(a.memberships || []) === JSON.stringify(b.memberships || []);
const planSession = () => plan && sessions.find((session) => session.retailer_id === plan.retailer_id && session.client_id === $('client').value && session.logged_in && contextEqual(session.context, plan.context));
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
    const messages = {400: 'Check your input, quantity and selected product.', 401: 'The service token is incorrect. Connect again.', 409: 'The plan changed or still needs review. Reopen it, resolve all lines, and review the warnings.', 502: 'The store session or interpreter is unavailable. Check the extension and store tab, then try again.', 504: 'The interpreter or catalog took too long. Please try again.'};
    throw new Error(messages[response.status] || result.error || 'The request failed.');
  }
  return result;
}

function message(text, error = false) {
  $('message').replaceChildren();
  if (text) $('message').append(el('p', text, `notice${error ? ' error' : ''}`));
}

function buttons() {
  $('create').disabled = busy || !token || (liveRetailer() && !selectedSession());
  $('pair-extension').disabled = busy || !token || !extensionReady;
  $('refresh').disabled = busy || !plan;
  $('transfer').disabled = busy || !plan || plan.status !== 'ready' || (plan.warnings.length > 0 && !$('ack').checked) || (plan.over_budget && !$('over').checked) || (retailers.find((r) => r.id === plan?.retailer_id)?.mode === 'browser' && !planSession());
  $('connect-form').querySelector('button').disabled = busy;
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
  const matchingSession = sessions.find((session) => session.retailer_id === plan.retailer_id && contextEqual(session.context, plan.context));
  if (matchingSession) $('client').value = matchingSession.client_id;
  $('transfer').textContent = simulated ? 'Send to demo basket ↗' : 'Fill store basket ↗';
  $('plan-meta').textContent = `${retailer?.name || plan.retailer_id} · ${plan.context.store_id} · ${plan.context.postal_code === '00000' ? 'Store delivery settings' : plan.context.postal_code} · ${plan.context.fulfillment} · Revision ${plan.revision}`;
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

$('connect-form').addEventListener('submit', (event) => { event.preventDefault(); action(async () => {
  token = $('token').value.trim();
  try {
    const [shops, clients, health] = await Promise.all([api('/v1/retailers', undefined, 'GET'), api('/v1/clients', undefined, 'GET'), api('/healthz', undefined, 'GET')]);
    retailers = shops.retailers; $('retailer').replaceChildren(); $('client').replaceChildren();
    for (const retailer of retailers) { const option = el('option', `${retailer.name}${retailer.mode === 'mock' ? ' · demo' : ''}`); option.value = retailer.id; $('retailer').append(option); }
    for (const client of clients.clients) { const option = el('option', client.id); option.value = client.id; $('client').append(option); }
    updateRetailer();
    $('connection-status').textContent = `Connected · ${health.interpreter === 'builtin' ? 'Built-in list parser' : 'AI text interpreter'} · ${health.persistent ? 'Plans saved on disk' : 'Plans kept in memory'}`;
    $('mode-note').textContent = `${health.interpreter === 'builtin' ? 'Built-in parser: lists and simple English or Swedish grocery requests. Meal planning needs the AI interpreter.' : 'AI interpretation may make assumptions. Review quantities and requirements.'} ${retailers.some((r) => r.mode === 'browser') ? 'Products and prices come from your store session. Review your basket at the store before checkout.' : 'Retailer data and basket actions in this demo are simulated.'}`;
    $('extension-setup').hidden = !retailers.some((r) => r.mode === 'browser');
    await refreshSessions();
    if (sessionPoll) clearInterval(sessionPoll);
    sessionPoll = setInterval(() => { if (token) refreshSessions().catch(() => { sessions = []; buttons(); }); }, 5000);
    sessionStorage.setItem('ship-it-token', token); await history();
  } catch (error) { token = ''; sessionStorage.removeItem('ship-it-token'); throw error; }
}); });

function updateRetailer() {
  const live = liveRetailer();
  $('session-wrap').hidden = !live;
  $('member-wrap').hidden = live;
  $('store').readOnly = live; $('postal').readOnly = live; $('fulfillment').disabled = live;
  if (!live) { $('store').value = `${$('retailer').value}-demo`; if ($('postal').value === '00000') $('postal').value = '11122'; }
  else applySession();
  buttons();
}
function applySession() {
  const session = selectedSession();
  if (session) {
    $('store').value = session.context.store_id;
    $('postal').value = session.context.postal_code;
    $('fulfillment').value = session.context.fulfillment;
    $('client').value = session.client_id;
  } else { $('store').value = ''; }
  buttons();
}
async function refreshSessions() {
  const result = await api('/v1/browser-sessions', undefined, 'GET');
  sessions = result.sessions;
  const previous = $('session').value;
  $('session').replaceChildren();
  const available = sessions.filter((session) => session.retailer_id === $('retailer').value);
  for (const session of available) {
    const option = el('option', `${session.name} · ${session.logged_in ? 'signed in' : 'sign in to fill basket'}`);
    option.value = `${session.client_id}:${session.context.session_id}`; $('session').append(option);
  }
  if (!available.length) $('session').append(el('option', 'Open this store in your connected browser'));
  if (available.some((session) => `${session.client_id}:${session.context.session_id}` === previous)) $('session').value = previous;
  if (liveRetailer()) applySession();
  if (extensionReady && available.length) $('extension-status').textContent = 'Store session connected. Sign in at the store before filling its basket.';
  buttons();
}
$('retailer').addEventListener('change', () => { updateRetailer(); refreshSessions().catch((error) => message(error.message, true)); });
$('session').addEventListener('change', applySession);
$('client').addEventListener('change', buttons);
document.addEventListener('ship-it-extension-ready', () => { extensionReady = true; $('extension-status').textContent = 'Extension detected. Connect it to receive grocery plans.'; buttons(); });
document.addEventListener('ship-it-paired', (event) => {
  try {
    const result = JSON.parse(event.detail);
    $('extension-status').textContent = result.error || `Connected as ${result.client_id}. Open your store tab to start.`;
    refreshSessions().catch(() => {});
  } catch { $('extension-status').textContent = 'Pairing failed. Reload the page and try again.'; }
});
$('pair-extension').addEventListener('click', () => { $('extension-status').textContent = 'Connecting…'; document.dispatchEvent(new CustomEvent('ship-it-pair')); });
document.dispatchEvent(new CustomEvent('ship-it-extension-probe'));
for (const button of document.querySelectorAll('[data-example]')) button.addEventListener('click', () => { $('text').value = examples[button.dataset.example]; $('text').focus(); });
$('ack').addEventListener('change', buttons); $('over').addEventListener('change', buttons);

$('plan-form').addEventListener('submit', (event) => { event.preventDefault(); action(async () => {
  message('Finding products and checking pack sizes…');
  const retailer = $('retailer').value;
  const context = liveRetailer() ? selectedSession()?.context : {store_id: $('store').value.trim(), postal_code: $('postal').value.trim(), fulfillment: $('fulfillment').value, memberships: $('member').checked ? [`${retailer}-demo-member`] : []};
  if (!context) throw new Error('Connect a store session in the extension first.');
  const required_traits = Array.from(document.querySelectorAll('.traits input:checked'), (input) => input.value);
  const result = await api('/v1/shopping-plans', {text: $('text').value, retailer_id: retailer, context, required_traits, budget_minor: Math.round(Number($('budget').value || 0) * 100)});
  showPlan(result); message(''); await history();
}); });

$('refresh').addEventListener('click', () => action(async () => { showPlan(await api(`/v1/shopping-plans/${plan.id}/refresh`, {revision: plan.revision})); await history(); }));

function showReceipt(transfer) {
  const simulated = retailers.find((r) => r.id === transfer.retailer_id)?.mode === 'mock';
  $('receipt').replaceChildren(el('p', `${simulated ? 'Demo basket' : 'Basket transfer'}: ${transfer.state.replaceAll('_', ' ')}`, 'notice'));
  if (transfer.receipt) for (const line of transfer.receipt.lines) $('receipt').append(el('p', `${line.product_id}: ${line.status.replaceAll('_', ' ')}${line.confirmed_quantity ? ` · ${line.confirmed_quantity} packs confirmed` : ''}${line.message ? ` · ${line.message}` : ''}`));
  if (!simulated) { const link = el('a', 'Review basket at the store ↗'); link.href = transfer.basket_url; link.target = '_blank'; link.rel = 'noopener noreferrer'; $('receipt').append(link); }
  $('receipt').append(el('p', simulated ? 'This is a simulated result. No real retailer basket was changed.' : 'Your browser client reports this result. Review your retailer basket before checkout.', 'muted'));
}

$('transfer').addEventListener('click', () => action(async () => {
  const scope = `ship-it-transfer:${plan.id}:${plan.revision}:${$('client').value}`;
  let key = sessionStorage.getItem(scope); if (!key) { key = crypto.randomUUID(); sessionStorage.setItem(scope, key); }
  const result = await api(`/v1/shopping-plans/${plan.id}/transfer`, {revision: plan.revision, client_id: $('client').value, acknowledge_warnings: $('ack').checked, allow_over_budget: $('over').checked}, 'POST', key);
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
if ($('token').value) $('connect-form').requestSubmit();
