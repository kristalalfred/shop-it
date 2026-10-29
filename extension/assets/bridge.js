'use strict';

(() => {
  if (globalThis.shopItBridgeLoaded) return;
  globalThis.shopItBridgeLoaded = true;
  const extension = globalThis.browser || globalThis.chrome;
  if (location.hostname !== '127.0.0.1' || location.protocol !== 'http:' || !['/', '/plugin'].includes(location.pathname)) return;
  async function connect() {
    let result = await extension.runtime.sendMessage({type: 'connection'});
    const token = document.getElementById('token')?.value;
    if (!result?.connected && token) {
      const paired = await extension.runtime.sendMessage({type: 'pair', url: location.origin, token});
      result = paired?.error ? {connected: false, error: paired.error} : {connected: true, client_id: paired.client_id};
    }
    return {connected: Boolean(result?.connected), client_id: result?.client_id || '', error: result?.error || ''};
  }
  const announce = () => connect().catch(() => ({connected: false, client_id: '', error: 'Reload the extension and this page.'})).then((result) => {
    document.dispatchEvent(new CustomEvent('shop-it-extension-ready', {detail: JSON.stringify(result)}));
  });
  document.addEventListener('shop-it-extension-probe', announce);
  document.addEventListener('shop-it-fill', () => { extension.runtime.sendMessage({type: 'fill'}).catch(() => {}); });
  announce();
})();
