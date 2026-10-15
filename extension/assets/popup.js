const extension = globalThis.browser || globalThis.chrome;
const $ = (id) => document.getElementById(id);
let url = 'http://127.0.0.1:18080';
async function update() {
  const result = await extension.runtime.sendMessage({type: 'status'});
  $('status').textContent = result.status;
  url = result.url;
}
$('open').addEventListener('click', () => extension.tabs.create({url}));
update().catch(() => { $('status').textContent = 'Reload the extension to reconnect.'; });
