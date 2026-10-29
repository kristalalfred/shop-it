# Shop It

Shop It turns a meal idea or a grocery list into a basket at Swedish online grocery stores. You plan the groceries with ChatGPT, which picks matching products at each store. Shop It compares basket totals and adds the basket you choose to the store's cart in your own browser. You review the cart and check out at the store.

Supported stores: Mathem, Willys and Hemköp.

Shop It runs on your computer. It has three parts:

- A ChatGPT plugin, installed through Codex, where you make and edit grocery lists.
- `shop-it`, a local service with a page at `http://127.0.0.1:24816` where you review saved lists and fill a store cart.
- A browser extension that searches the stores and fills store carts in your signed-in browser session.

Shop It is not affiliated with Mathem or Axfood. It uses the stores' website endpoints, which can change without notice.

## Requirements

- Go 1.26 or newer to build `shop-it`.
- Chrome, Edge, Brave or another Chromium browser, or Firefox 142 or newer.
- An account at each store you want to fill a cart at.
- Codex, to install the ChatGPT plugin.

## Install

```sh
go install github.com/kristalalfred/shop-it/cmd/shop-it@latest
shop-it
```

The first run creates a configuration file and prints the address it listens on. Open that address in your browser.

### Browser extension

The Shop It page shows the downloads while the extension is not connected. They are also available from the running service:

| Browser | Download | Install |
| --- | --- | --- |
| Firefox, Zen | `http://127.0.0.1:24816/extension/firefox.zip` | Open `about:debugging#/runtime/this-firefox`, choose **Load Temporary Add-on**, and select the downloaded file. Firefox removes temporary add-ons when it restarts. |
| Chrome, Edge, Brave | `http://127.0.0.1:24816/extension/chromium.zip` | Unzip the download, open `chrome://extensions`, enable **Developer mode**, choose **Load unpacked**, and select the folder. |

The extension pairs with the service the next time you open the Shop It page.

### ChatGPT plugin

```sh
shop-it -plugin
codex plugin marketplace add "<plugin directory printed by shop-it>"
codex plugin add shop-it@shop-it-local
```

`shop-it -plugin` writes a local plugin marketplace next to the configuration file. The plugin contains a copy of the `shop-it` binary. Run the three commands again after upgrading `shop-it`.

The plugin starts the service if it is not running. Its log is `plugin.log` next to the configuration file.

## Use

1. Sign in at each store in the browser that has the extension, and choose a delivery address or pickup store there.
2. In ChatGPT, ask Shop It to plan groceries, for example `Plan korvstroganoff for 5 people and compare my stores.` The first time, it opens a setup view for choosing and connecting stores.
3. ChatGPT picks matching products at each store. Ask it to choose another product, search again or skip an item.
4. Open the list on the Shop It page with **Review and fill** in the chat, or at `http://127.0.0.1:24816`. Earlier lists are under **Saved lists**.
5. Choose a store and select **Fill basket**. The extension opens the store in a new tab and adds each item.
6. Review the cart and check out at the store.

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

Command-line flags:

| Flag | Description |
| --- | --- |
| `-config <path>` | Use another configuration file. Only the default file is created automatically. |
| `-listen <address>` | Override `listen`. |
| `-plugin` | Write the ChatGPT plugin and exit. |

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
| `just publish-firefox` | Submit the Firefox extension to addons.mozilla.org. Reads `AMO_JWT_ISSUER` and `AMO_JWT_SECRET` from `.data/.env`. |

`just check` needs Node.js and [Hurl](https://hurl.dev).

## License

[MIT](LICENSE)
