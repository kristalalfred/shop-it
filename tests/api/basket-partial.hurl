POST {{base_url}}/v1/basket-transfers
Authorization: Bearer {{user_token}}
Content-Type: application/json
Idempotency-Key: basket-1

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
[Asserts]
jsonpath "$.state" == "pending"
header "Location" == "/v1/basket-transfers/{{transfer_id}}"
jsonpath "$.basket_url" == "https://handla.ica.se/"

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
jsonpath "$.items.bananas" == 3
jsonpath "$.items['milk-basic']" not exists

POST {{base_url}}/v1/basket-transfers
Authorization: Bearer {{user_token}}
Content-Type: application/json
Idempotency-Key: basket-1

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
HTTP 200
[Asserts]
jsonpath "$.id" == "{{transfer_id}}"

POST {{io_url}}/browser/step
Authorization: Bearer {{client_token}}
HTTP 200
[Asserts]
jsonpath "$.claimed" == true

GET {{base_url}}/v1/basket-transfers/{{transfer_id}}
Authorization: Bearer {{user_token}}
HTTP 200
[Asserts]
jsonpath "$.state" == "partial"
jsonpath "$.receipt.lines[0].product_id" == "milk-basic"
jsonpath "$.receipt.lines[0].confirmed_quantity" == 2
jsonpath "$.receipt.lines[0].status" == "applied"
jsonpath "$.receipt.lines[1].status" == "unavailable"

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
jsonpath "$.items.*" count == 2
jsonpath "$.items.bananas" == 3
jsonpath "$.items['milk-basic']" == 2
jsonpath "$.items['milk-lactose-free']" not exists

POST {{io_url}}/browser/step
Authorization: Bearer {{client_token}}
HTTP 200
[Asserts]
jsonpath "$.claimed" == false

POST {{base_url}}/v1/basket-transfers
Authorization: Bearer {{user_token}}
Content-Type: application/json
Idempotency-Key: basket-1

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
HTTP 200
[Asserts]
jsonpath "$.id" == "{{transfer_id}}"
jsonpath "$.state" == "partial"

POST {{base_url}}/v1/basket-transfers
Authorization: Bearer {{user_token}}
Content-Type: application/json
Idempotency-Key: basket-1

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
      "target_quantity": 3
    },
    {
      "product_id": "milk-lactose-free",
      "target_quantity": 1
    }
  ]
}
HTTP 409
[Asserts]
jsonpath "$.error" == "conflict"

GET {{io_url}}/catalog/requests
HTTP 200
[Asserts]
jsonpath "$.requests" count == 0
