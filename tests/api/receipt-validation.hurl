POST {{base_url}}/v1/basket-transfers
Authorization: Bearer {{user_token}}
Content-Type: application/json
Idempotency-Key: receipt

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

PUT {{base_url}}/v1/client/transfers/{{transfer_id}}/result
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
      "status": "unavailable",
      "confirmed_quantity": 0
    }
  ]
}
HTTP 409

POST {{base_url}}/v1/client/claims
Authorization: Bearer {{client_token}}
HTTP 200
[Asserts]
jsonpath "$.id" == "{{transfer_id}}"

PUT {{base_url}}/v1/client/transfers/{{transfer_id}}/result
Authorization: Bearer {{client_token}}
Content-Type: application/json

{
  "lines": []
}
HTTP 400

PUT {{base_url}}/v1/client/transfers/{{transfer_id}}/result
Authorization: Bearer {{client_token}}
Content-Type: application/json

{
  "lines": [
    {
      "product_id": "milk-basic",
      "status": "applied",
      "confirmed_quantity": 2
    }
  ]
}
HTTP 400

PUT {{base_url}}/v1/client/transfers/{{transfer_id}}/result
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
      "product_id": "milk-basic",
      "status": "applied",
      "confirmed_quantity": 2
    }
  ]
}
HTTP 400

PUT {{base_url}}/v1/client/transfers/{{transfer_id}}/result
Authorization: Bearer {{client_token}}
Content-Type: application/json

{
  "lines": [
    {
      "product_id": "milk-basic",
      "status": "applied",
      "confirmed_quantity": 1
    },
    {
      "product_id": "milk-lactose-free",
      "status": "unavailable",
      "confirmed_quantity": 0
    }
  ]
}
HTTP 400

PUT {{base_url}}/v1/client/transfers/{{transfer_id}}/result
Authorization: Bearer {{client_token}}
Content-Type: application/json

{
  "lines": [
    {
      "product_id": "milk-basic",
      "status": "invented",
      "confirmed_quantity": 2
    },
    {
      "product_id": "milk-lactose-free",
      "status": "unavailable",
      "confirmed_quantity": 0
    }
  ]
}
HTTP 400

PUT {{base_url}}/v1/client/transfers/{{transfer_id}}/result
Authorization: Bearer {{client_token}}
Content-Type: application/json

{
  "lines": [
    {
      "product_id": "milk-basic",
      "status": "failed",
      "confirmed_quantity": 2
    },
    {
      "product_id": "milk-lactose-free",
      "status": "unavailable",
      "confirmed_quantity": 0
    }
  ]
}
HTTP 400

PUT {{base_url}}/v1/client/transfers/{{transfer_id}}/result
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
      "status": "unavailable",
      "confirmed_quantity": 0
    }
  ]
}
HTTP 200
[Asserts]
jsonpath "$.state" == "partial"

PUT {{base_url}}/v1/client/transfers/{{transfer_id}}/result
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
      "status": "unavailable",
      "confirmed_quantity": 0
    }
  ]
}
HTTP 200
[Asserts]
jsonpath "$.state" == "partial"

PUT {{base_url}}/v1/client/transfers/{{transfer_id}}/result
Authorization: Bearer {{client_token}}
Content-Type: application/json

{
  "lines": [
    {
      "product_id": "milk-lactose-free",
      "status": "unavailable",
      "confirmed_quantity": 0
    },
    {
      "product_id": "milk-basic",
      "status": "applied",
      "confirmed_quantity": 2
    }
  ]
}
HTTP 200
[Asserts]
jsonpath "$.state" == "partial"

PUT {{base_url}}/v1/client/transfers/{{transfer_id}}/result
Authorization: Bearer {{client_token}}
Content-Type: application/json

{
  "lines": [
    {
      "product_id": "milk-basic",
      "status": "failed",
      "confirmed_quantity": 0
    },
    {
      "product_id": "milk-lactose-free",
      "status": "unavailable",
      "confirmed_quantity": 0
    }
  ]
}
HTTP 409
