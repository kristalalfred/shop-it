POST {{base_url}}/v1/basket-transfers
Authorization: Bearer {{user_token}}
Content-Type: application/json
Idempotency-Key: outcome

{
  "retailer_id": "ica",
  "client_id": "demo-browser",
  "context": {
    "store_id": "ica-demo",
    "postal_code": "11122",
    "fulfillment": "delivery",
    "delivery_slot_id": "demo-slot",
    "memberships": [
      "ica-demo-member"
    ]
  },
  "items": [
    {
      "product_id": "milk-basic",
      "target_quantity": 2
    },
    {
      "product_id": "milk-lactose-free",
      "target_quantity": 1
    }
  ]
}
HTTP 202
[Captures]
transfer_id: jsonpath "$.id"

POST {{io_url}}/browser/step
Authorization: Bearer {{client_token}}
HTTP 200
[Asserts]
jsonpath "$.claimed" == true

GET {{base_url}}/v1/basket-transfers/{{transfer_id}}
Authorization: Bearer {{user_token}}
HTTP 200
[Asserts]
jsonpath "$.state" == "{{state}}"
jsonpath "$.receipt.lines[0].status" == "{{line_status}}"
jsonpath "$.receipt.lines" count == 2

POST {{io_url}}/basket
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
  }
}
HTTP 200
[Asserts]
jsonpath "$.item_count" toString == "{{basket_count}}"

POST {{io_url}}/browser/step
Authorization: Bearer {{client_token}}
HTTP 200
[Asserts]
jsonpath "$.claimed" == false
