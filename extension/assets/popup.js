const extension = globalThis.browser || globalThis.chrome;
const $ = (id) => document.getElementById(id);
let url = 'http://127.0.0.1:18080';
async function update() {
  const result = await extension.runtime.sendMessage({type: 'status'});
  $('status').textContent = result.status;
  $('enabled').checked = result.enabled;
  url = result.url;
  $('sessions').replaceChildren();
  for (const session of result.sessions) {
    const line = document.createElement('p');
    line.textContent = `${session.name} · ${session.logged_in ? 'signed in' : 'sign in to fill basket'}`;
    $('sessions').append(line);
  }
}
$('enabled').addEventListener('change', async () => {
  const result = await extension.runtime.sendMessage({type: 'enabled', enabled: $('enabled').checked});
  if (result.error) $('status').textContent = result.error;
  else await update();
});
$('open').addEventListener('click', () => extension.tabs.create({url}));
update().catch(() => { $('status').textContent = 'Reload the extension to reconnect.'; });
