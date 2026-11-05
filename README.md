# Shop It

Shop It turns a meal idea or a grocery list into a basket at Swedish online grocery stores. You plan the groceries with ChatGPT, which picks matching products at each store. Shop It compares basket totals and adds the basket you choose to the store's cart in your own browser. You review the cart and check out at the store.

Supported stores: Mathem, Willys and Hemköp.

Shop It runs on your computer. It has three parts:

- `shop-it`, a local app with a page at `http://127.0.0.1:24816` where you set Shop It up, review saved lists and fill a store cart.
- A browser extension that searches the stores and fills store carts in your signed-in browser session.
- A ChatGPT plugin for the Codex app, where you make and edit grocery lists.

Shop It is not affiliated with Mathem or Axfood. It uses the stores' website endpoints, which can change without notice.

## Install

Install Go 1.26 or newer, then:

```sh
go install github.com/kristalalfred/shop-it/cmd/shop-it@latest
shop-it
```

`shop-it` opens its page in your browser, which sets up the rest:

1. **Browser extension.** The page shows where to load it in Chrome, Edge, Brave, Firefox 142 or newer, or Zen. Firefox removes temporary add-ons when it restarts.
2. **ChatGPT.** **Add to ChatGPT** opens the Shop It plugin in the Codex app. Install it there.
3. **Stores.** Choose the stores you shop at. Sign in and choose a delivery address or pickup store at each one in the same browser.

Each step is checked when it is done. Run `shop-it` again at any time to open the page. When Codex uses the plugin, it starts `shop-it` if it is not running. Its log is `plugin.log` next to the configuration file.

## Use

1. Select **Make a list in ChatGPT** on the Shop It page, or ask Shop It in the Codex app, for example `Plan korvstroganoff for 5 people and compare my stores.`
2. ChatGPT picks matching products at each store. Ask it to choose another product, search again or skip an item.
3. The list appears on the Shop It page. Choose a store and select **Fill basket**. The extension opens the store in a new tab and adds each item.
4. Review the cart and check out at the store.

Earlier lists are under **Saved lists**. **Setup** shows the setup steps again.

## Configuration

The configuration file is created on first run:

| Platform | Path |
| --- | --- |
| macOS | `~/Library/Application Support/shop-it/config.json` |
| Linux | `$XDG_CONFIG_HOME/shop-it/config.json`, or `~/.config/shop-it/config.json` |
| Windows | `%AppData%\shop-it\config.json` |

| Key | Description |
| --- | --- |
| `listen` | Loopback address and port. Default `127.0.0.1:24816`. |
| `api_token` | Token for the Shop It page and plugin. Generated on first run. |
| `clients` | Browser extension identities and tokens. Generated on first run. |
| `retailers` | Stores to compare. Each entry has `id`, `name`, `mode` and `basket_url`. |
| `adapter_timeout_ms` | Timeout for each store search, 1 to 30000. |
| `data_dir` | Directory for saved plans and basket transfers, relative to the configuration file. Default `data`. |

Shop It also writes the ChatGPT plugin to `plugin` and the unpacked browser extensions to `extension` next to the configuration file each time it starts.

Command-line flags:

| Flag | Description |
| --- | --- |
| `-config <path>` | Use another configuration file. Only the default file is created automatically. |
| `-listen <address>` | Override `listen`. |
| `-plugin` | Write the ChatGPT plugin and exit. |
| `-version` | Print the version and exit. |

One service can use a data directory at a time. Shop It keeps the 30 most recently updated plans.

## Privacy

Shop It sends no data to the developer. See [PRIVACY.md](PRIVACY.md) for what the app and extension read, store and send.

## Development

The [`justfile`](justfile) has the common tasks:

| Command | Description |
| --- | --- |
| `just build` | Build the binaries into `bin/` and unpacked extensions into `bin/extension/`. |
| `just check` | Run formatting, vet, JavaScript tests, Hurl API tests and Go tests. |
| `just test [case]` | Run all tests, or one test harness case by name. |
| `just run` | Run the service with your configuration. |
| `just run demo` | Run with simulated stores from `config/dev.json`. |
| `just install` | Install `shop-it` with `go install` and write the ChatGPT plugin. |
| `just package` | Build release archives for macOS and Linux into `bin/dist/` from a clean, committed tree. |
| `just publish-firefox` | Submit the Firefox extension to addons.mozilla.org. Reads `AMO_JWT_ISSUER` and `AMO_JWT_SECRET` from `.data/.env`. |

`just check` needs Node.js and [Hurl](https://hurl.dev).

## License

[MIT](LICENSE)
