'use strict';

(() => {
  if (globalThis.shopItBridgeLoaded) return;
  globalThis.shopItBridgeLoaded = true;
  const extension = globalThis.browser || globalThis.chrome;
  if (location.hostname !== '127.0.0.1' || location.protocol !== 'http:' || location.pathname !== '/') return;
  async function connect() {
    const token = document.getElementById('token')?.value;
    const result = await extension.runtime.sendMessage(token ? {type: 'pair', url: location.origin, token} : {type: 'connection'});
    return {connected: Boolean(result?.client_id), client_id: result?.client_id || '', error: result?.error || ''};
  }
  const announce = () => connect().catch(() => ({connected: false, client_id: '', error: 'Reload the extension and this page.'})).then((result) => {
    document.dispatchEvent(new CustomEvent('shop-it-extension-ready', {detail: JSON.stringify(result)}));
  });
  document.addEventListener('shop-it-extension-probe', announce);
  document.addEventListener('shop-it-fill', () => { extension.runtime.sendMessage({type: 'fill'}).catch(() => {}); });
  announce();
})();
