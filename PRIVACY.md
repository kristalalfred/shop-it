# Privacy

Shop It consists of the `shop-it` app, which runs on your computer, and the Shop It browser extension. Neither sends data to the developer. There are no analytics, tracking or advertising.

## Browser extension

The extension reads and changes your cart at Mathem, Willys and Hemköp, using your existing sign-in at those stores. It does this only when the Shop It app on your computer asks for it, for example when you select **Fill basket**.

| Data | Use | Sent to |
| --- | --- | --- |
| Products and quantities in your store cart | Confirm which items were added | The Shop It app on your computer |
| The store, delivery address postal code and delivery slot selected at the store | Compare prices for the store you shop at, and stop a fill if the store changed | The Shop It app on your computer |
| Product search results from the store | Match products to your grocery list | The Shop It app on your computer |
| A pairing token for the Shop It app | Authenticate requests to the app | Kept in the extension's local storage |
| A record of items added during a fill | Resume an interrupted fill without adding items twice | Kept in the extension's local storage |

The extension communicates only with the store websites listed above and with the Shop It app at `http://127.0.0.1`. It does not read store passwords or payment details, and it never checks out or pays.

## Shop It app

- The app accepts connections only from your own computer, and every request needs a token from its configuration file.
- Plans, settings and basket results are stored in the app's data folder on your computer.
- To search store catalogs, the app sends search terms to the store websites.
- If you configure an AI interpreter, your shopping request and product names are sent to that provider: OpenAI through the Codex CLI or the OpenAI API, under your own account. The `builtin` interpreter sends nothing.
- With the ChatGPT plugin, ChatGPT receives your shopping request, your plan and product search results, as part of your conversation.

## Contact

Report privacy questions as an issue at <https://github.com/kristalalfred/shop-it/issues>.
