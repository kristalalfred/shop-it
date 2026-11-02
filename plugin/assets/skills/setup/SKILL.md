---
name: setup
description: Set up Shop It grocery planning or resume an interrupted store connection after installing the Shop It plugin.
---

Call `open_shop_it` to read the real setup state and display the app. If the user already supplied a shopping request, save it as `draft` with `configure_shop_it` before anything else. Keep it through setup and reconnects.

Setup happens on the Shop It page at the returned `setup_url`. It installs the browser extension and lets the user choose and sign in to their stores. When `browser_online` is false or no selected store is ready, open `setup_url` for the user and wait for them to finish there. Do not claim an extension is installed or a store is signed in or connected until the tools confirm it. Do not collect store passwords in chat.

Call `connect_store` for each selected store before planning. It checks the store in the user's browser and reads its selected store or delivery context. If it reports `login_required` or `setup_required`, ask the user to sign in or choose delivery or pickup at that store, then check again. Stores can be checked one at a time; keep the request and the stores that are ready when one fails. To compare other stores, save the user's choice with `configure_shop_it` and `retailer_ids`.

Once a store is ready, continue the user's request. Translate it into concrete ingredients for `create_shopping_plan`, using Swedish product search queries and amounts in g, ml, each, or pack. Put dietary requirements only on affected ingredients. Use returned products as evidence for prices and availability. Match ingredients at each store, then submit the confirmed candidate IDs together with `select_products` to calculate comparable baskets in one revision. Use `update_plan_line` for individual changes and read the current revision after edits. Leave uncertain matches unresolved for review.

The visual app and tools share a plan. Respect direct user edits. Explain remaining choices in the conversation rather than adding instructional text to the app. You cannot change a store basket: the user reviews the plan and fills the basket on the Shop It page at `review_url`. Resolve or explicitly skip missing products first, and disclose unverified requirements and any budget overrun. The user checks out at the store.
