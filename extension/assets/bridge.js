'use strict';

(() => {
  if (globalThis.shipItBridgeLoaded) return;
  globalThis.shipItBridgeLoaded = true;
  const extension = globalThis.browser || globalThis.chrome;
  if (location.hostname !== '127.0.0.1' || location.protocol !== 'http:' || location.pathname !== '/') return;
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
    document.dispatchEvent(new CustomEvent('ship-it-extension-ready', {detail: JSON.stringify(result)}));
  });
  document.addEventListener('ship-it-extension-probe', announce);
  announce();
})();
