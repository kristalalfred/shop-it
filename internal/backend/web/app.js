'use strict';

const $ = (id) => document.getElementById(id);
const money = (m) => new Intl.NumberFormat('sv-SE', {style: 'currency', currency: m.currency}).format(m.amount_minor / 100);
const el = (tag, text, cls) => { const node = document.createElement(tag); if (text != null) node.textContent = text; if (cls) node.className = cls; return node; };
let token = $('token').value.trim(), plan = null, busy = false, setup = null, savedPlans = [], poll = null, watch = null, fill = null, showSetup = false;
let browserExtension = {ready: false, connected: false, error: ''};
const checking = new Set();
const firefox = /Firefox\//.test(globalThis.navigator?.userAgent || '');
const stores = () => setup?.stores || [];
const shopName = (id) => stores().find((store) => store.id === id)?.name || id;
const planRetailer = () => plan ? stores().find((store) => store.id === plan.retailer_id) : null;
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

const extensionConnected = () => browserExtension.connected || Boolean(setup?.browser_online);
const storeReady = (store) => store.mode !== 'browser' || store.session?.status === 'ready';
const storesReady = () => stores().some((store) => store.selected && storeReady(store));
const setupDone = () => Boolean(setup) && (setup.demo || extensionConnected()) && setup.chatgpt_connected && storesReady();

function mark(id, done) { $(id).className = `step${done ? ' done' : ''}`; }

function copyButton(text) {
  const button = el('button', text, 'copy'); button.type = 'button';
  button.addEventListener('click', () => {
    globalThis.navigator?.clipboard?.writeText(text).then(() => {
      button.className = 'copy copied';
      setTimeout(() => { button.className = 'copy'; }, 1500);
    }).catch(() => {});
  });
  return button;
}

function recipe() {
  const dir = setup.extension_dir, sep = dir?.includes('\\') ? '\\' : '/';
  const target = firefox ? [dir, 'firefox', 'manifest.json'] : [dir, 'chromium'];
  const load = el('li', null), page = el('li', null);
  page.append(copyButton(firefox ? 'about:debugging#/runtime/this-firefox' : 'chrome://extensions'));
  load.append(el('span', firefox ? 'Load Temporary Add-on…' : 'Developer mode → Load unpacked', 'recipe-action'));
  if (dir) load.append(copyButton(target.join(sep)));
  else { const link = el('a', firefox ? 'firefox.zip' : 'chromium.zip', 'button'); link.href = `/extension/${firefox ? 'firefox' : 'chromium'}.zip`; load.append(link); }
  return [page, load];
}

function renderStore(store) {
  const ready = store.selected && storeReady(store);
  const card = el('div', null, `store${store.selected ? ' selected' : ''}${ready ? ' ready' : ''}`);
  const toggle = el('button', store.name, 'store-name'); toggle.type = 'button';
  toggle.setAttribute('aria-pressed', String(store.selected));
  toggle.disabled = checking.has(store.id);
  toggle.addEventListener('click', () => chooseStore(store));
  card.append(toggle);
  if (!store.selected) return card;
  const state = el('div', null, 'store-state'), status = store.session?.status;
  if (checking.has(store.id)) state.append(el('span', 'Checking…', 'muted'));
  else if (ready) state.append(el('span', store.session?.label || store.name, 'store-label'));
  else {
    if (status === 'login_required' || status === 'setup_required') {
      const link = el('a', status === 'login_required' ? 'Sign in ↗' : 'Choose delivery ↗', 'store-link');
      link.href = store.url; link.target = '_blank'; link.rel = 'noopener noreferrer';
      state.append(link);
    }
    const check = el('button', status ? 'Check again' : 'Check', 'small'); check.type = 'button';
    check.disabled = !extensionConnected();
    check.addEventListener('click', () => checkStore(store.id));
    state.append(check);
  }
  card.append(state);
  return card;
}

function renderSetup() {
  if (!setup) return;
  const extensionDone = setup.demo || extensionConnected();
  $('step-extension').hidden = setup.demo;
  mark('step-extension', extensionDone);
  $('extension-status').textContent = extensionDone ? 'Connected' : browserExtension.ready ? browserExtension.error || 'Reload the extension, then this page.' : '';
  $('extension-install').hidden = extensionDone || browserExtension.ready;
  if (!$('extension-install').hidden) { const steps = el('ol'); steps.append(...recipe()); $('extension-install').replaceChildren(steps); }
  mark('step-chatgpt', setup.chatgpt_connected);
  $('chatgpt-status').textContent = setup.chatgpt_connected ? 'Added' : '';
  $('install').hidden = setup.chatgpt_connected || !setup.install_url;
  if (setup.install_url) $('install').href = setup.install_url;
  mark('step-stores', storesReady());
  $('stores').replaceChildren(...stores().map(renderStore));
  const startable = setup.chatgpt_connected && Boolean(setup.chat_url);
  $('start').className = `button${setupDone() ? ' primary' : ''}${startable ? '' : ' disabled'}`;
  if (startable) { $('start').href = setup.chat_url; $('start').removeAttribute('aria-disabled'); }
  else { $('start').removeAttribute('href'); $('start').setAttribute('aria-disabled', 'true'); }
  layout();
}

function layout() {
  const listing = Boolean(plan);
  $('setup').hidden = !setup || listing && !showSetup && (setup.demo || extensionConnected());
  $('setup-title').hidden = listing;
  $('start').hidden = listing || !setup?.chat_url;
  $('setup-toggle').hidden = !listing;
  $('setup-toggle').className = setupDone() ? '' : 'attention';
  $('setup-toggle').setAttribute('aria-expanded', String(!$('setup').hidden));
  $('new-list').hidden = !listing || !setup?.chat_url;
  if (setup?.chat_url) $('new-list').href = setup.chat_url;
  $('saved-lists').hidden = !savedPlans.length;
  $('intro').hidden = !listing;
}

async function chooseStore(store) {
  const ids = stores().filter((s) => s.id === store.id ? !s.selected : s.selected).map((s) => s.id);
  try {
    setup = await api('/v1/setup/stores', {retailer_ids: ids}, 'PUT');
    renderSetup();
    if (!store.selected && store.mode === 'browser' && extensionConnected()) await checkStore(store.id);
  } catch (error) { message(error.message, true); }
}

async function checkStore(id) {
  checking.add(id); renderSetup(); message('');
  try { setup = await api(`/v1/setup/stores/${encodeURIComponent(id)}/check`); }
  catch (error) { message(error.message, true); }
  finally { checking.delete(id); renderSetup(); }
}

async function refreshSetup() {
  setup = await api('/v1/setup', undefined, 'GET');
  renderSetup();
}

const filling = () => ['pending', 'claimed'].includes(fill?.state);
const unverified = (line) => !line.skipped && entry(line).selected?.unmet_traits?.length > 0;
const needsAck = () => plan.warnings.length > 0 || plan.lines.some(unverified);

function buttons() {
  if (!plan) return;
  const finished = Boolean(fill) && !filling(), completed = fill?.state === 'completed', shop = planRetailer();
  const live = shop?.mode === 'browser';
  $('refresh').hidden = finished;
  $('refresh').disabled = busy || filling() || (live && !extensionConnected());
  $('refresh').textContent = busy === 'refresh' ? 'Checking prices…' : 'Refresh prices';
  $('transfer').className = completed ? '' : 'primary';
  $('transfer').textContent = busy === 'fill' && !fill ? 'Checking the store…' : filling() ? 'Filling…' : finished ? 'Fill basket again' : !shop ? 'Fill basket' : shop.mode === 'mock' ? `Fill ${shop.name} demo basket` : `Fill ${shop.name} basket`;
  $('transfer').disabled = Boolean(busy) || filling() || chosenBasket().status !== 'ready' || (needsAck() && !$('ack').checked) || (chosenBasket().over_budget && !$('over').checked) || (live && !extensionConnected());
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
  layout();
  buttons();
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
  savedPlans = result.plans;
  $('history').replaceChildren();
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
  layout();
  return result.plans;
}

async function connect() {
  const [current, plans] = await Promise.all([api('/v1/setup', undefined, 'GET'), api('/v1/shopping-plans', undefined, 'GET')]);
  setup = current;
  let opened = null;
  for (const id of [new URLSearchParams(window.location.search).get('plan'), plans.plans[0]?.id].filter(Boolean)) {
    try { opened = await api(`/v1/shopping-plans/${encodeURIComponent(id)}`, undefined, 'GET'); break; } catch {}
  }
  if (opened) showPlan(opened);
  await loadLists();
  renderSetup();
  watch = setTimeout(follow, plan && setupDone() ? 5000 : 2000);
}

async function follow() {
  if (!busy && !filling()) {
    try {
      await refreshSetup();
      const plans = await loadLists();
      if (!plan && plans.length) showPlan(await api(`/v1/shopping-plans/${plans[0].id}`, undefined, 'GET'));
      else if (plan && plans.find((saved) => saved.id === plan.id)?.updated_at !== plan.updated_at) showPlan(await api(`/v1/shopping-plans/${plan.id}`, undefined, 'GET'));
    } catch {}
  }
  watch = setTimeout(follow, plan && setupDone() ? 5000 : 2000);
}

document.addEventListener('shop-it-extension-ready', (event) => {
  let detail = {};
  try { detail = JSON.parse(event.detail || '{}'); } catch {}
  browserExtension = {ready: true, connected: detail.connected === true, error: detail.error || ''};
  renderSetup();
  buttons();
});
document.dispatchEvent(new CustomEvent('shop-it-extension-probe'));
document.addEventListener('visibilitychange', () => { if (document.visibilityState === 'visible' && setup) refreshSetup().catch(() => {}); });
$('setup-toggle').addEventListener('click', () => { showSetup = $('setup').hidden; layout(); });
$('ack').addEventListener('change', buttons); $('over').addEventListener('change', buttons);

$('refresh').addEventListener('click', () => action('refresh', async () => { fill = null; showPlan(await api(`/v1/shopping-plans/${plan.id}/refresh`, {revision: plan.revision})); await loadLists(); }));

function showFill(transfer) {
  fill = transfer;
  const retailer = stores().find((r) => r.id === transfer.retailer_id);
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

window.addEventListener('pagehide', () => { stopPoll(); clearTimeout(watch); });
action('open', connect);
