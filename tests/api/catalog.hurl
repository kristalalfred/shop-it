POST {{base_url}}/v1/catalog/search
Authorization: Bearer {{user_token}}
Content-Type: application/json

{
  "retailer_id": "{{retailer}}",
  "context": {
    "store_id": "{{retailer}}-demo",
    "postal_code": "11122",
    "fulfillment": "delivery",
    "delivery_slot_id": "demo-slot",
    "memberships": [
      "{{retailer}}-demo-member"
    ]
  },
  "query": "mjölk"
}
HTTP 200
[Asserts]
header "Content-Type" == "application/json"
jsonpath "$.retailer_id" == "{{retailer}}"
jsonpath "$.products" count == 2
jsonpath "$.products[0].id" == "milk-basic"
jsonpath "$.products[0].price.amount_minor" toString == "{{price}}"
jsonpath "$.products[0].price.currency" == "SEK"
jsonpath "$.context.store_id" == "{{retailer}}-demo"
jsonpath "$.context.postal_code" == "11122"
jsonpath "$.context.fulfillment" == "delivery"
jsonpath "$.context.delivery_slot_id" == "demo-slot"
jsonpath "$.context.memberships[0]" == "{{retailer}}-demo-member"

GET {{io_url}}/catalog/requests
HTTP 200
[Asserts]
jsonpath "$.requests" count == 1
jsonpath "$.requests[0].retailer_id" == "{{retailer}}"
jsonpath "$.requests[0].context.store_id" == "{{retailer}}-demo"
jsonpath "$.requests[0].context.delivery_slot_id" == "demo-slot"
jsonpath "$.requests[0].context.memberships[0]" == "{{retailer}}-demo-member"
jsonpath "$.requests[0].query" == "mjölk"
