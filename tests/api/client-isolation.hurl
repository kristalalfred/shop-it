POST {{base_url}}/v1/basket-transfers
Authorization: Bearer {{user_token}}
Content-Type: application/json
Idempotency-Key: shared-key

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
first_id: jsonpath "$.id"

POST {{base_url}}/v1/basket-transfers
Authorization: Bearer {{user_token}}
Content-Type: application/json
Idempotency-Key: shared-key

{
  "retailer_id": "ica",
  "client_id": "other-browser",
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
second_id: jsonpath "$.id"
[Asserts]
jsonpath "$.id" != "{{first_id}}"

POST {{base_url}}/v1/client/claims
Authorization: Bearer {{user_token}}
HTTP 401

POST {{base_url}}/v1/client/claims
Authorization: Bearer {{other_token}}
HTTP 200
[Asserts]
jsonpath "$.id" == "{{second_id}}"
jsonpath "$.client_id" == "other-browser"

PUT {{base_url}}/v1/client/transfers/{{first_id}}/result
Authorization: Bearer {{other_token}}
Content-Type: application/json

{
  "lines": [
    {
      "product_id": "milk-basic",
      "status": "applied",
      "confirmed_quantity": 2
    },
    {
      "product_id": "milk-lactose-free",
      "status": "applied",
      "confirmed_quantity": 1
    }
  ]
}
HTTP 404

PUT {{base_url}}/v1/client/transfers/{{second_id}}/result
Authorization: Bearer {{client_token}}
Content-Type: application/json

{
  "lines": [
    {
      "product_id": "milk-basic",
      "status": "applied",
      "confirmed_quantity": 2
    },
    {
      "product_id": "milk-lactose-free",
      "status": "applied",
      "confirmed_quantity": 1
    }
  ]
}
HTTP 404

PUT {{base_url}}/v1/client/transfers/{{second_id}}/result
Authorization: Bearer {{other_token}}
Content-Type: application/json

{
  "lines": [
    {
      "product_id": "milk-basic",
      "status": "applied",
      "confirmed_quantity": 2
    },
    {
      "product_id": "milk-lactose-free",
      "status": "applied",
      "confirmed_quantity": 1
    }
  ]
}
HTTP 200
[Asserts]
jsonpath "$.state" == "completed"

POST {{base_url}}/v1/client/claims
Authorization: Bearer {{other_token}}
HTTP 204

POST {{base_url}}/v1/client/claims
Authorization: Bearer {{client_token}}
HTTP 200
[Asserts]
jsonpath "$.id" == "{{first_id}}"
jsonpath "$.client_id" == "demo-browser"
