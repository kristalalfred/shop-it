POST {{base_url}}/v1/catalog/search
Authorization: Bearer {{user_token}}
Content-Type: application/json

{
  "retailer_id": "ica",
  "context": {
    "store_id": "ica-demo",
    "postal_code": "11122",
    "fulfillment": "delivery",
    "delivery_slot_id": "demo-slot",
    "memberships": [
      "ica-demo-member"
    ]
  },
  "query": "mjölk"
}
HTTP 200
[Captures]
cursor: jsonpath "$.next_cursor"
[Asserts]
jsonpath "$.observed_at" == "2026-10-14T12:00:00Z"
jsonpath "$.products[1].offers[0].min_quantity" == 2
jsonpath "$.products[1].offers[0].membership" == "ica-demo-member"
jsonpath "$.products[1].traits.gluten_free" == "unknown"

POST {{base_url}}/v1/catalog/search
Authorization: Bearer {{user_token}}
Content-Type: application/json

{
  "retailer_id": "ica",
  "context": {
    "store_id": "ica-demo",
    "postal_code": "11122",
    "fulfillment": "delivery",
    "delivery_slot_id": "demo-slot",
    "memberships": [
      "ica-demo-member"
    ]
  },
  "query": "mjölk",
  "cursor": "{{cursor}}"
}
HTTP 200
[Asserts]
jsonpath "$.products" count == 2
jsonpath "$.products[0].availability" == "out_of_stock"
jsonpath "$.products[1].availability" == "unknown"
jsonpath "$.next_cursor" not exists

POST {{base_url}}/v1/catalog/search
Authorization: Bearer {{user_token}}
Content-Type: application/json

{
  "retailer_id": "ica",
  "context": {
    "store_id": "ica-demo",
    "postal_code": "11122",
    "fulfillment": "delivery",
    "delivery_slot_id": "demo-slot",
    "memberships": [
      "ica-demo-member"
    ]
  },
  "query": "nonexistent food"
}
HTTP 200
[Asserts]
jsonpath "$.products" count == 0

POST {{base_url}}/v1/catalog/search
Authorization: Bearer {{user_token}}
Content-Type: application/json

{
  "retailer_id": "ica",
  "context": {
    "store_id": "ica-demo",
    "postal_code": "11122",
    "fulfillment": "delivery",
    "delivery_slot_id": "demo-slot",
    "memberships": [
      "ica-demo-member"
    ]
  },
  "query": "bananer"
}
HTTP 200
[Asserts]
jsonpath "$.products[0].pack.quantity" == 500
jsonpath "$.products[0].pack.unit" == "g"
jsonpath "$.products[0].price_kind" == "estimated"

POST {{base_url}}/v1/catalog/search
Authorization: Bearer {{user_token}}
Content-Type: application/json

{
  "retailer_id": "ica",
  "context": {
    "store_id": "ica-demo",
    "postal_code": "11122",
    "fulfillment": "pickup",
    "delivery_slot_id": "",
    "memberships": []
  },
  "query": "mjölk"
}
HTTP 200
[Asserts]
jsonpath "$.context.fulfillment" == "pickup"
jsonpath "$.products[0].id" == "milk-basic"
