'use strict';

const $ = (id) => document.getElementById(id);
const money = (m) => new Intl.NumberFormat('sv-SE', {style: 'currency', currency: m.currency}).format(m.amount_minor / 100);
const el = (tag, text, cls) => { const node = document.createElement(tag); if (text != null) node.textContent = text; if (cls) node.className = cls; return node; };
let token = $('token').value.trim(), plan = null, busy = false, retailers = [], poll = null, watch = null, fill = null;
let browserExtension = {ready: false, connected: false, error: ''};
const shopName = (id) => retailers.find((retailer) => retailer.id === id)?.name || id;
const planRetailer = () => plan ? retailers.find((retailer) => retailer.id === plan.retailer_id) : null;
const chosenBasket = (value = plan) => value.baskets.find((basket) => basket.retailer_id === value.retailer_id);
const entry = (line) => line.baskets[plan.retailer_id] || {candidates: []};

const generic = new Set(['conflict', 'invalid request', 'not found', 'adapter unavailable', 'adapter timeout', 'internal error', 'unauthorized']);
const failures = {400: 'This list could not be used. Reload the page.', 401: 'The connection expired. Reload this page to reconnect.', 404: 'This list no longer exists.', 409: 'The list changed or still needs review. Check it and try again.', 502: 'The shop did not respond. Try again.', 504: 'The shop took too long to respond. Try again.'};
const failure = (status, error) => new Error(error && !generic.has(error) ? error : failures[status] || 'The request failed.');

async function api(path, body, method = 'POST', key) {
  const headers = {Authorization: `Bearer ${token}`, Accept: 'application/json'};
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  if (key) headers['Idempotency-Key'] = key;
  const response = await fetch(path, {method, headers, body: body === undefined ? undefined : JSON.stringify(body)});
  const result = await response.json();
  if (!response.ok) throw failure(response.status, result.error);
  return result;
}

function message(text, error = false) {
  $('message').replaceChildren();
  if (text) $('message').append(el('p', text, `notice${error ? ' error' : ''}`));
}

const filling = () => ['pending', 'claimed'].includes(fill?.state);
const unverified = (line) => !line.skipped && entry(line).selected?.unmet_traits?.length > 0;
const needsAck = () => plan.warnings.length > 0 || plan.lines.some(unverified);

function buttons() {
  if (!plan) return;
  const finished = Boolean(fill) && !filling(), completed = fill?.state === 'completed', shop = planRetailer();
  const live = shop?.mode === 'browser';
  $('refresh').hidden = finished;
  $('refresh').disabled = busy || filling() || (live && !browserExtension.connected);
  $('refresh').textContent = busy === 'refresh' ? 'Checking prices…' : 'Refresh prices';
  $('transfer').className = completed ? '' : 'primary';
  $('transfer').textContent = busy === 'fill' && !fill ? 'Checking the store…' : filling() ? 'Filling…' : finished ? 'Fill basket again' : !shop ? 'Fill basket' : shop.mode === 'mock' ? `Fill ${shop.name} demo basket` : `Fill ${shop.name} basket`;
  $('transfer').disabled = Boolean(busy) || filling() || chosenBasket().status !== 'ready' || (needsAck() && !$('ack').checked) || (chosenBasket().over_budget && !$('over').checked) || (live && !browserExtension.connected);
  $('filled').hidden = !completed;
  $('review-store').hidden = !finished;
  for (const card of $('baskets').children) card.disabled = Boolean(busy) || filling();
}

async function action(kind, work) {
  if (busy) return;
  busy = kind; buttons(); message('');
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

function showPlan(value) {
  const changed = plan?.id !== value.id || plan?.revision !== value.revision;
  if (plan?.id !== value.id) { stopPoll(); fill = null; }
  plan = value;
  if (changed) { $('ack').checked = false; $('over').checked = false; }
  if (new URLSearchParams(window.location.search).get('plan') !== plan.id) window.history.replaceState(null, '', `/?plan=${encodeURIComponent(plan.id)}`);
  $('title').textContent = plan.text;
  $('subtitle').textContent = `Saved ${new Date(plan.updated_at).toLocaleString(undefined, {dateStyle: 'medium', timeStyle: 'short'})}`;
  $('start')?.setAttribute('hidden', '');
  $('review').hidden = false; $('checkout').hidden = false; $('list').hidden = false;
  const basket = chosenBasket(), missing = plan.lines.filter(needsAttention);
  const hasProducts = plan.lines.some((line) => !line.skipped && entry(line).selected?.eligible);
  renderBaskets();
  $('missing').textContent = !missing.length ? '' : hasProducts ? `${missing.length === 1 ? '1 item has' : `${missing.length} items have`} no product and won’t be added. ${missing.length === 1 ? 'It is' : 'They are'} at the top of the list.`
    : `Nothing on this list has a product at ${shopName(plan.retailer_id)}.`;
  $('missing').hidden = !$('missing').textContent;
  $('notes').replaceChildren();
  if (plan.warnings.length) {
    const list = el('ul');
    for (const warning of plan.warnings) list.append(el('li', warning));
    $('notes').append(el('h3', 'Assumptions'), list);
  }
  $('ack-wrap').hidden = !needsAck();
  $('ack-text').textContent = plan.warnings.length ? 'I’ve reviewed the assumptions.' : 'I’ve reviewed the requirements no product is marked with.';
  $('over-wrap').hidden = !basket.over_budget;
  if (basket.over_budget) $('over-text').textContent = `Accept going ${money({amount_minor: basket.total.amount_minor - plan.budget_minor, currency: basket.total.currency})} over your ${money({amount_minor: plan.budget_minor, currency: basket.total.currency})} budget.`;
  $('total').textContent = money(basket.total);
  $('estimate').textContent = `${basket.estimated ? 'Estimated by weight. ' : ''}Delivery excluded. ${planRetailer()?.mode === 'mock' ? 'Demo prices.' : 'Confirm prices at checkout.'}`;
  renderList();
  updateExtension();
}

function showEmpty() {
  plan = null;
  $('title').textContent = 'No lists yet.';
  $('subtitle').textContent = '';
  $('start')?.removeAttribute('hidden');
  $('review').hidden = true; $('checkout').hidden = true; $('list').hidden = true;
}

function renderBaskets() {
  $('shops').hidden = plan.baskets.length < 2;
  $('baskets').replaceChildren(...plan.baskets.map((basket) => {
    const chosen = basket.retailer_id === plan.retailer_id;
    const card = el('button', null, `basket${chosen ? ' chosen' : ''}`); card.type = 'button';
    card.setAttribute('aria-pressed', String(chosen));
    card.append(el('span', shopName(basket.retailer_id), 'basket-name'), el('strong', money(basket.total), 'basket-total'), el('span', basket.missing ? `${basket.missing} missing` : 'Everything found', 'basket-detail'));
    card.addEventListener('click', () => { if (!chosen) action('choose', async () => { fill = null; showPlan(await api(`/v1/shopping-plans/${plan.id}`, {revision: plan.revision, retailer_id: basket.retailer_id}, 'PATCH')); await loadLists(); }); });
    return card;
  }));
}

function renderList() {
  const groups = [['Needs attention', plan.lines.filter(needsAttention)], ['Matched', plan.lines.filter((line) => !line.skipped && !needsAttention(line))], ['Skipped', plan.lines.filter((line) => line.skipped)]].filter(([, lines]) => lines.length);
  $('list-count').textContent = `${plan.lines.length === 1 ? '1 item' : `${plan.lines.length} items`} · ${shopName(plan.retailer_id)}`;
  $('lines').replaceChildren(...groups.flatMap(([title, lines]) => [...(groups.length > 1 ? [el('h3', `${title} · ${lines.length}`, 'group')] : []), ...lines.map(renderLine)]));
}

function renderLine(line) {
  const node = el('div', null, `line${line.skipped ? ' skipped' : needsAttention(line) ? ' attention' : ''}`), text = el('span', null, 'line-text');
  const selected = entry(line).selected, result = notAdded(line) && fillResult(line);
  const detail = line.skipped ? '' : result ? `${selected.target_quantity} × ${selected.product.name} · ${result.message || 'Not added to the basket.'}`
    : needsAttention(line) ? entry(line).issue || 'No product chosen.'
    : [`${selected.target_quantity} × ${selected.product.name}`, unmarked(selected)].filter(Boolean).join(' · ');
  text.append(el('span', line.item.label, 'line-name'), el('span', [amount(line.item), detail].filter(Boolean).join(' · '), 'line-detail'));
  const flag = line.skipped ? el('span', 'Skipped', 'line-flag') : result ? el('span', fillLabels[result.status] || 'Check', 'line-flag') : needsAttention(line) ? el('span', entry(line).candidates.length ? 'Not chosen' : 'Not found', 'line-flag') : el('span', money(selected.total), 'line-price');
  node.append(text, flag);
  return node;
}

async function loadLists() {
  const result = await api('/v1/shopping-plans', undefined, 'GET');
  $('history').replaceChildren();
  if (!result.plans.length) $('history').textContent = 'No saved lists.';
  for (const saved of result.plans) {
    const button = el('button', `${saved.text.slice(0, 55)}${saved.text.length > 55 ? '…' : ''} · ${shopName(saved.retailer_id)} ${money(saved.total)}`); button.type = 'button';
    if (saved.id === plan?.id) button.setAttribute('aria-current', 'true');
    button.addEventListener('click', () => action('open', async () => {
      showPlan(await api(`/v1/shopping-plans/${saved.id}`, undefined, 'GET'));
      $('saved-lists').open = false;
      window.scrollTo({top: 0, behavior: 'smooth'});
    }));
    $('history').append(button);
  }
  return result.plans;
}

async function connect() {
  const [shops, plans] = await Promise.all([api('/v1/retailers', undefined, 'GET'), api('/v1/shopping-plans', undefined, 'GET')]);
  retailers = shops.retailers;
  let opened = null;
  for (const id of [new URLSearchParams(window.location.search).get('plan'), plans.plans[0]?.id].filter(Boolean)) {
    try { opened = await api(`/v1/shopping-plans/${encodeURIComponent(id)}`, undefined, 'GET'); break; } catch {}
  }
  if (opened) showPlan(opened); else showEmpty();
  await loadLists();
  updateExtension();
  watch = setInterval(follow, 5000);
}

async function follow() {
  if (busy || filling()) return;
  try {
    const plans = await loadLists();
    if (!plan && plans.length) showPlan(await api(`/v1/shopping-plans/${plans[0].id}`, undefined, 'GET'));
    else if (plan && plans.find((saved) => saved.id === plan.id)?.updated_at !== plan.updated_at) showPlan(await api(`/v1/shopping-plans/${plan.id}`, undefined, 'GET'));
  } catch {}
}

function updateExtension() {
  const live = retailers.some((retailer) => retailer.mode === 'browser');
  $('extension-setup').hidden = !live || browserExtension.connected;
  $('extension-status').textContent = browserExtension.error || (browserExtension.ready ? 'The Shop It extension is not connected. Reload the extension and this page.' : 'Install the Shop It extension to fill your basket, then reload this page.');
  $('extension-install').hidden = browserExtension.ready;
  buttons();
}
document.addEventListener('shop-it-extension-ready', (event) => {
  let detail = {};
  try { detail = JSON.parse(event.detail || '{}'); } catch {}
  browserExtension = {ready: true, connected: detail.connected === true, error: detail.error || ''};
  updateExtension();
});
document.dispatchEvent(new CustomEvent('shop-it-extension-probe'));
$('ack').addEventListener('change', buttons); $('over').addEventListener('change', buttons);

$('refresh').addEventListener('click', () => action('refresh', async () => { fill = null; showPlan(await api(`/v1/shopping-plans/${plan.id}/refresh`, {revision: plan.revision})); await loadLists(); }));

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
  $('review-store').href = transfer.basket_url;
  renderList();
  buttons();
}

$('transfer').addEventListener('click', () => action('fill', async () => {
  const scope = `shop-it-transfer:${plan.id}:${plan.revision}`;
  let key = sessionStorage.getItem(scope);
  if (!key) { key = crypto.randomUUID(); sessionStorage.setItem(scope, key); }
  fill = null;
  const result = await api(`/v1/shopping-plans/${plan.id}/transfer`, {revision: plan.revision, acknowledge_warnings: $('ack').checked, allow_over_budget: $('over').checked}, 'POST', key);
  document.dispatchEvent(new CustomEvent('shop-it-fill'));
  const track = (transfer) => {
    showFill(transfer);
    if (filling()) poll = setTimeout(check, 1000);
    else sessionStorage.removeItem(scope);
  };
  const check = async () => {
    try { track(await api(`/v1/basket-transfers/${result.id}`, undefined, 'GET')); } catch (error) { message(error.message, true); }
  };
  stopPoll();
  track(result);
}));

window.addEventListener('pagehide', () => { stopPoll(); clearInterval(watch); });
action('open', connect);
