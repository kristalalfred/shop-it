POST {{base_url}}/v1/basket-transfers
Authorization: Bearer {{user_token}}
Content-Type: application/json

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
HTTP 400

POST {{base_url}}/v1/basket-transfers
Authorization: Bearer {{user_token}}
Content-Type: application/json
Idempotency-Key: invalid

{
  "retailer_id": "ica",
  "client_id": "missing",
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
HTTP 400

POST {{base_url}}/v1/basket-transfers
Authorization: Bearer {{user_token}}
Content-Type: application/json
Idempotency-Key: invalid

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
  "items": []
}
HTTP 400

POST {{base_url}}/v1/basket-transfers
Authorization: Bearer {{user_token}}
Content-Type: application/json
Idempotency-Key: invalid

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
      "target_quantity": 0
    }
  ]
}
HTTP 400

POST {{base_url}}/v1/basket-transfers
Authorization: Bearer {{user_token}}
Content-Type: application/json
Idempotency-Key: invalid

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
      "target_quantity": -1
    }
  ]
}
HTTP 400

POST {{base_url}}/v1/basket-transfers
Authorization: Bearer {{user_token}}
Content-Type: application/json
Idempotency-Key: invalid

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
      "product_id": "",
      "target_quantity": 2
    }
  ]
}
HTTP 400

POST {{base_url}}/v1/basket-transfers
Authorization: Bearer {{user_token}}
Content-Type: application/json
Idempotency-Key: invalid

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
      "product_id": "milk-basic",
      "target_quantity": 2
    }
  ]
}
HTTP 400

POST {{base_url}}/v1/basket-transfers
Authorization: Bearer {{user_token}}
Content-Type: application/json
Idempotency-Key: invalid

{
  "retailer_id": "unknown",
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
HTTP 404

GET {{base_url}}/v1/basket-transfers/missing
Authorization: Bearer {{user_token}}
HTTP 404

POST {{base_url}}/v1/client/claims
Authorization: Bearer {{client_token}}
HTTP 204
