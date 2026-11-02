# Chrome Web Store listing

Upload `bin/extension/chromium.zip`, built by `just build`.

## Store listing

| Field | Value |
| --- | --- |
| Category | Shopping |
| Language | English |
| Store icon | `extension/assets/icon-128.png` |
| Screenshots | `extension/store/screenshot-compare.png`, `extension/store/screenshot-filling.png` |
| Small promo tile | `extension/store/promo-small.png` |
| Homepage URL | https://github.com/kristalalfred/shop-it |
| Support URL | https://github.com/kristalalfred/shop-it/issues |

Description:

```
Shop It turns a meal idea or a grocery list into a basket at Swedish online grocery stores. You plan the groceries with ChatGPT, Shop It compares what the basket costs at Mathem, Willys and Hemköp, and this extension adds the basket you choose to the store's cart in your browser. You review the cart and check out at the store.

The extension works together with the free Shop It app, which runs on your computer, and its ChatGPT plugin. Install the app with Homebrew (brew install kristalalfred/tap/shop-it) and run shop-it. The extension connects to its page automatically.

- Uses your existing sign-in at Mathem, Willys and Hemköp.
- Searches the stores in your browser, so prices match your own store and delivery address.
- Shows progress on the store page while it adds items, and lists any item it could not add.
- Never checks out or pays.
- Sends no data to the developer. The extension talks only to the stores and to the Shop It app on your computer.

Shop It is an independent open-source project and is not affiliated with Mathem or Axfood.
```

## Privacy practices

Single purpose:

```
Finds and adds the groceries planned with the Shop It app on the user's computer to the user's cart at Mathem, Willys or Hemköp.
```

| Permission | Justification |
| --- | --- |
| `storage` | Stores the pairing token for the Shop It app on the user's computer, and a record of items added during a fill so an interrupted fill does not add items twice. |
| `alarms` | Wakes the extension every 30 seconds to check whether the Shop It app has a store search or a basket to fill. |
| Host permission `http://127.0.0.1/*` | Connects to the Shop It app running on the user's own computer. |
| Host permissions for mathem.se, willys.se and hemkop.se | Reads and updates the user's cart, searches products and reads the selected store or delivery address at these grocery stores. |

Remote code: No.

Data usage: select **Location** (delivery postal code) and **Website content** (cart contents and product search results). Certify that data is not sold, not used for unrelated purposes and not used for creditworthiness.

Privacy policy URL: https://github.com/kristalalfred/shop-it/blob/main/PRIVACY.md

## Test instructions

```
The extension is the browser half of Shop It, an open-source app that runs locally: https://github.com/kristalalfred/shop-it. It talks only to that app on http://127.0.0.1 and to mathem.se, willys.se and hemkop.se.

Lists are made in ChatGPT, so testing needs the Codex app signed in with a ChatGPT account. No store account is needed:
1. Install Shop It on macOS or Linux: brew install kristalalfred/tap/shop-it. Run shop-it. Its page opens at http://127.0.0.1:24816.
2. Load this extension in Chrome. The page marks the extension as connected.
3. Select Add to ChatGPT and install the Shop It plugin in the Codex app.
4. Select Willys on the page. The extension opens willys.se. Choose a store for pickup there, then select Check again on the page.
5. Select Make a list in ChatGPT and ask: "Plan 2 litres of milk and 12 eggs at Willys." The extension searches willys.se and the list appears on the Shop It page.
6. Select Fill Willys basket on the Shop It page.
7. The extension adds the items to the Willys cart and shows a progress dialog. Willys accepts items in an anonymous cart.

The source in this package is not minified or generated.
```
