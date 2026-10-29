# Ship It

Ship It turns a meal idea or a grocery list into a basket at Swedish online grocery stores. It finds matching products at each store, compares basket totals, and adds the basket you choose to the store's cart in your own browser. You review the cart and check out at the store.

Supported stores: Mathem, Willys and Hemköp.

Ship It runs on your computer. It has three parts:

- `ship-it`, a local service with a web app at `http://127.0.0.1:18080`.
- A browser extension that fills store carts in your signed-in browser session.
- An optional ChatGPT plugin, installed through Codex, that plans groceries in a chat and shares the plan with the web app.

Ship It is not affiliated with Mathem or Axfood. It uses the stores' website endpoints, which can change without notice.

## Requirements

- Go 1.26 or newer to build `ship-it`.
- Chrome, Edge, Brave or another Chromium browser, or Firefox 142 or newer.
- An account at each store you want to fill a cart at.
- For meal planning from free text, one of:
  - the [Codex CLI](https://github.com/openai/codex), signed in with `codex login`, or
  - an OpenAI API key.

Without either, the web app reads explicit grocery lists such as `2 litres milk, 12 eggs and 500 g bananas`.

## Install

```sh
go install github.com/kristalalfred/ship-it/cmd/ship-it@latest
ship-it
```

The first run creates a configuration file and prints the address it listens on. Open that address in your browser.

### Browser extension

The web app shows the downloads while the extension is not connected. They are also available from the running service:

| Browser | Download | Install |
| --- | --- | --- |
| Firefox, Zen | `http://127.0.0.1:18080/extension/firefox.zip` | Open `about:debugging#/runtime/this-firefox`, choose **Load Temporary Add-on**, and select the downloaded file. Firefox removes temporary add-ons when it restarts. |
| Chrome, Edge, Brave | `http://127.0.0.1:18080/extension/chromium.zip` | Unzip the download, open `chrome://extensions`, enable **Developer mode**, choose **Load unpacked**, and select the folder. |

The extension pairs with the service the next time you open the web app.

### ChatGPT plugin

```sh
ship-it -plugin
codex plugin marketplace add "<plugin directory printed by ship-it>"
codex plugin add ship-it@ship-it-local
```

`ship-it -plugin` writes a local plugin marketplace next to the configuration file. The plugin contains a copy of the `ship-it` binary. Run the three commands again after upgrading `ship-it`.

The plugin starts the service if it is not running. Its log is `plugin.log` next to the configuration file.

## Use

1. Sign in at each store in the browser that has the extension, and choose a delivery address or pickup store there.
2. In the web app, describe what you need, for example `Korvstroganoff för 5 personer. Vi har redan salt och peppar.`, and optionally set a budget.
3. Review the matched products. Choose another product, change the search, or skip an item.
4. Choose a store and select **Fill basket**. The extension opens the store in a new tab and adds each item.
5. Review the cart and check out at the store.

In ChatGPT, ask the plugin to plan groceries, for example `Plan five dinners for two and compare my stores.` The plugin opens a setup view for choosing and connecting stores, then creates a plan you can review and edit in the chat.

## Configuration

The configuration file is created on first run:

| Platform | Path |
| --- | --- |
| macOS | `~/Library/Application Support/ship-it/config.json` |
| Linux | `$XDG_CONFIG_HOME/ship-it/config.json`, or `~/.config/ship-it/config.json` |
| Windows | `%AppData%\ship-it\config.json` |

| Key | Description |
| --- | --- |
| `listen` | Loopback address and port. Default `127.0.0.1:18080`. |
| `api_token` | Token for the web app and plugin. Generated on first run. |
| `clients` | Browser extension identities and tokens. Generated on first run. |
| `retailers` | Stores to compare. Each entry has `id`, `name`, `mode` and `basket_url`. |
| `adapter_timeout_ms` | Timeout for each store search, 1 to 30000. |
| `data_dir` | Directory for saved plans and basket transfers, relative to the configuration file. Default `data`. |
| `interpreter.mode` | How the web app reads free text: `auto`, `codex`, `openai` or `builtin`. Default `auto`. |
| `interpreter.model` | Model name. Required for `openai`, optional for `codex`. |

Interpreter modes:

| Mode | Behavior |
| --- | --- |
| `auto` | Uses `codex` when the Codex CLI is on `PATH`, otherwise `builtin`. |
| `codex` | Runs `codex exec` in a read-only sandbox. Requires `codex login`. |
| `openai` | Calls the OpenAI Responses API with the key in `OPENAI_API_KEY`. |
| `builtin` | Reads explicit lists with amounts. Makes no AI calls. |

The ChatGPT plugin does not use the interpreter. ChatGPT supplies the ingredients and product choices.

Command-line flags:

| Flag | Description |
| --- | --- |
| `-config <path>` | Use another configuration file. Only the default file is created automatically. |
| `-listen <address>` | Override `listen`. |
| `-interpreter <mode>` | Override `interpreter.mode`. |
| `-plugin` | Write the ChatGPT plugin and exit. |

One service can use a data directory at a time. Ship It keeps the 30 most recently updated plans.

## Privacy

- The service accepts connections only on the loopback interface, and every API request needs a token from the configuration file.
- Store sign-in stays in your browser. Ship It does not read or store store passwords.
- In the web app, the service searches each store's public catalog. Prices can differ from your chosen store until you open the cart.
- With the ChatGPT plugin, searches run in your browser through the extension, using the store and delivery settings you chose at the store.
- Your shopping request and product names are sent to the interpreter you configure: Codex or OpenAI. The `builtin` interpreter sends nothing.
- Ship It never checks out or pays.

## Development

The [`justfile`](justfile) has the common tasks:

| Command | Description |
| --- | --- |
| `just build` | Build the binaries into `bin/` and unpacked extensions into `bin/extension/`. |
| `just check` | Run formatting, vet, JavaScript tests, Hurl API tests and Go tests. |
| `just test [case]` | Run all tests, or one test harness case by name. |
| `just run` | Run the service with your configuration. |
| `just run demo` | Run with simulated stores from `config/dev.json`. |
| `just install` | Install `ship-it` with `go install` and write the ChatGPT plugin. |

`just check` needs Node.js and [Hurl](https://hurl.dev).

## License

[MIT](LICENSE)
