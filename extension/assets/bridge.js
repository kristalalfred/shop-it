'use strict';

(() => {
  if (globalThis.shipItBridgeLoaded) return;
  globalThis.shipItBridgeLoaded = true;
  const extension = globalThis.browser || globalThis.chrome;
  if (location.hostname !== '127.0.0.1' || location.protocol !== 'http:' || location.pathname !== '/') return;
  const announce = () => document.dispatchEvent(new CustomEvent('ship-it-extension-ready'));
  document.addEventListener('ship-it-pair', () => {
    const token = document.getElementById('token')?.value;
    if (!token) return;
    extension.runtime.sendMessage({type: 'pair', url: location.origin, token}).then((result) => {
      document.dispatchEvent(new CustomEvent('ship-it-paired', {detail: JSON.stringify(result)}));
    }, () => document.dispatchEvent(new CustomEvent('ship-it-paired', {detail: JSON.stringify({error: 'Open the extension to reconnect.'})})));
  });
  document.addEventListener('ship-it-extension-probe', announce);
  announce();
})();
