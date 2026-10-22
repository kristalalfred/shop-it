---
name: setup
description: Set up Ship It grocery planning or resume an interrupted store connection after installing the Ship It plugin.
---

Call `open_ship_it` to read the real setup state and display the app. If the user already supplied a shopping request, save it with `configure_ship_it` before starting setup. Keep it through reconnects.

Use the returned stores and connection status. Let the user choose their stores in the app or conversation. Save only their chosen retailer IDs. Ask only for missing shopping constraints that affect the requested plan.

When the browser is offline, open the returned `setup_url` for the user and guide them through its browser setup controls. The first development build uses a locally loaded browser extension. Do not claim an extension is installed, signed in, or connected until the tools confirm it.

Call `connect_store` for each chosen store. It opens that store in the paired browser and reads its selected store or delivery context. If sign-in or store selection is needed, let the user complete it at the retailer, then check again. Do not collect retailer passwords in chat. Stores can be connected one at a time; retain the request and other successful connections when one fails.

Once a store is ready, continue the user's request. Translate it into concrete ingredients for `create_shopping_plan`, using Swedish product search queries and amounts in g, ml, each, or pack. Put dietary requirements only on affected ingredients. Use returned products as evidence for prices and availability. Match ingredients at each store, then submit the confirmed candidate IDs together with `select_products` to calculate comparable baskets in one revision. Use `update_plan_line` for individual changes and read the current revision after edits. Leave uncertain matches unresolved for review.

The visual app and tools share a plan. Respect direct user edits. Explain remaining choices in the conversation rather than adding instructional text to the app. Fill the retailer basket only when the user requests it. Resolve or explicitly skip missing products first, disclose unverified requirements and any budget overrun, and reuse the same request ID if retrying a basket transfer. Read its receipt before reporting success. The user checks out at the retailer.
