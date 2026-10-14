'use strict';

(() => {
  if (globalThis.shipItContentLoaded) return;
  globalThis.shipItContentLoaded = true;
  const extension = globalThis.browser || globalThis.chrome;
  const id = Object.keys(ShipItAdapters.origins).find((key) => ShipItAdapters.origins[key] === location.origin);
  if (!id) return;
  const nativeFetch = globalThis.fetch.bind(globalThis);
  async function request(path, options = {}) {
    if (!path.startsWith('/') || path.startsWith('//')) throw new Error('Invalid retailer path.');
    const headers = {Accept: 'application/json'};
    if (options.body !== undefined) {
      headers['Content-Type'] = 'application/json';
      if (id === 'mathem') {
        const csrf = document.cookie.split('; ').find((cookie) => cookie.startsWith('csrftoken='))?.slice(10);
        if (!csrf) throw new Error('Open Mathem and sign in before sending a basket.');
        headers['X-CSRFToken'] = decodeURIComponent(csrf);
        headers['X-Client-App'] = 'tienda-web';
      } else {
        const csrfPath = id === 'willys' ? '/axfood/rest/v2/csrf-token' : '/axfood/rest/csrf-token';
        const response = await nativeFetch(csrfPath, {credentials: 'include', signal: AbortSignal.timeout(10000)});
        if (!response.ok) throw new Error('Could not read the store session.');
        headers['X-CSRF-Token'] = await response.json();
      }
    }
    const response = await nativeFetch(path, {method: options.method || 'GET', headers, credentials: 'include', redirect: 'error', signal: AbortSignal.timeout(10000), ...(options.body !== undefined ? {body: JSON.stringify(options.body)} : {})});
    if (!response.ok) throw Object.assign(new Error(`Store request failed (${response.status}).`), {status: response.status === 401 ? 'login_required' : 'failed'});
    return response.json();
  }
  const adapter = ShipItAdapters.create(id, {request});
  extension.runtime.onMessage.addListener((message, sender, respond) => {
    if (sender.id !== extension.runtime.id || message?.type !== 'store-operation' || message.retailer_id !== id) return false;
    const methods = {session: () => adapter.session(), search: () => adapter.search(message.job), read: () => adapter.read(message.context), set: () => adapter.set(message.context, message.item, message.before)};
    if (!Object.hasOwn(methods, message.operation)) return false;
    Promise.resolve().then(methods[message.operation]).then((result) => respond({result}), (error) => respond({error: error.message, status: error.status || 'failed'}));
    return true;
  });
})();
