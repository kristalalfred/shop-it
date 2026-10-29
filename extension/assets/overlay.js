'use strict';

(() => {
  if (globalThis.ShopItOverlay) return;
  const styles = `
    :host { all: initial; }
    .card { box-sizing: border-box; width: min(400px, calc(100vw - 32px)); margin: auto; padding: 24px; background: #fff; color: #171717; border: 2px solid #171717; border-radius: 0; box-shadow: 6px 6px 0 #171717; font: 15px/1.5 Arial, Helvetica, sans-serif; outline: none; }
    .card::backdrop { background: rgb(23 23 23 / .35); }
    .brand { margin: 0; font-size: 14px; font-weight: 900; letter-spacing: -.5px; }
    .title { margin: 12px 0 4px; font-size: 21px; font-weight: 800; letter-spacing: -.5px; line-height: 1.2; }
    .detail { margin: 0; min-height: 1.5em; font-size: 13px; overflow-wrap: anywhere; }
    .bar { height: 14px; margin-top: 18px; border: 2px solid #171717; background: #fff; }
    .fill { height: 100%; width: 0; background: #ffe64d; transition: width .3s ease-out; }
    .count { margin: 8px 0 0; font-size: 12px; color: #595959; }
    .finished .fill { background: #9be37c; }
    @media (prefers-reduced-motion: reduce) { .fill { transition: none; } }
  `;
  let host = null, parts = null, watchdog = null;
  const element = (tag, className, parent) => {
    const node = document.createElement(tag);
    node.className = className;
    parent.append(node);
    return node;
  };
  function remove() {
    clearTimeout(watchdog);
    watchdog = null;
    host?.remove();
    host = null;
    parts = null;
  }
  function show(state) {
    if (!host) {
      host = document.createElement('shop-it-overlay');
      const root = host.attachShadow({mode: 'closed'});
      element('style', '', root).textContent = styles;
      const card = element('dialog', 'card', root);
      card.setAttribute('aria-live', 'polite');
      card.addEventListener('cancel', (event) => event.preventDefault());
      element('p', 'brand', card).textContent = 'shop it';
      parts = {card, title: element('p', 'title', card), detail: element('p', 'detail', card), fill: element('div', 'fill', element('div', 'bar', card)), count: element('p', 'count', card)};
      document.documentElement.append(host);
      card.showModal();
    }
    parts.title.textContent = state.title;
    parts.detail.textContent = state.detail || '';
    parts.fill.style.width = `${Math.round(Math.min(Math.max(state.progress || 0, 0), 1) * 100)}%`;
    parts.count.textContent = state.count || '';
    parts.card.className = state.finished ? 'card finished' : 'card';
    clearTimeout(watchdog);
    watchdog = setTimeout(remove, 60000);
  }
  globalThis.ShopItOverlay = {show, remove};
})();
