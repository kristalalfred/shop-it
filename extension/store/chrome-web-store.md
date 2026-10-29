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
Shop It turns a meal idea or a grocery list into a basket at Swedish online grocery stores. It compares what the basket costs at Mathem, Willys and Hemköp, and this extension adds the basket you choose to the store's cart in your browser. You review the cart and check out at the store.

The extension works together with the free Shop It app, which runs on your computer. Install the app from https://github.com/kristalalfred/shop-it and open it in your browser. The extension connects to it automatically.

- Uses your existing sign-in at Mathem, Willys and Hemköp.
- Shows progress on the store page while it adds items, and lists any item it could not add.
- Never checks out or pays.
- Sends no data to the developer. The extension talks only to the stores and to the Shop It app on your computer.

Shop It is an independent open-source project and is not affiliated with Mathem or Axfood.
```

## Privacy practices

Single purpose:

```
Adds the groceries planned in the Shop It app on the user's computer to the user's cart at Mathem, Willys or Hemköp.
```

| Permission | Justification |
| --- | --- |
| `storage` | Stores the pairing token for the Shop It app on the user's computer, and a record of items added during a fill so an interrupted fill does not add items twice. |
| `alarms` | Wakes the extension every 30 seconds to check whether the Shop It app has a basket to fill. |
| Host permission `http://127.0.0.1/*` | Connects to the Shop It app running on the user's own computer. |
| Host permissions for mathem.se, willys.se and hemkop.se | Reads and updates the user's cart, searches products and reads the selected store or delivery address at these grocery stores. |

Remote code: No.

Data usage: select **Location** (delivery postal code) and **Website content** (cart contents and product search results). Certify that data is not sold, not used for unrelated purposes and not used for creditworthiness.

Privacy policy URL: https://github.com/kristalalfred/shop-it/blob/main/PRIVACY.md

## Test instructions

```
The extension is the browser half of Shop It, an open-source app that runs locally: https://github.com/kristalalfred/shop-it. It talks only to that app on http://127.0.0.1 and to mathem.se, willys.se and hemkop.se.

To test without a store account:
1. Install Go 1.26 or newer, then run: go install github.com/kristalalfred/shop-it/cmd/shop-it@latest
2. Run shop-it and open http://127.0.0.1:24816 in Chrome with the extension installed.
3. Enter "2 litres milk and 12 eggs", select Find groceries, choose Willys, and select Fill Willys basket.
4. The extension opens willys.se, adds the items to the cart and shows a progress dialog. Willys accepts items in an anonymous cart.

The source in this package is not minified or generated.
```
