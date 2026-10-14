POST {{base_url}}/v1/catalog/search
Authorization: Bearer {{user_token}}
Content-Type: application/json

```
{
```
HTTP 400
[Asserts]
jsonpath "$.error" == "invalid request"

POST {{base_url}}/v1/catalog/search
Authorization: Bearer {{user_token}}
Content-Type: application/json

```
{} {}
```
HTTP 400
[Asserts]
jsonpath "$.error" == "invalid request"

POST {{base_url}}/v1/catalog/search
Authorization: Bearer {{user_token}}
Content-Type: application/json

```
null
```
HTTP 400
[Asserts]
jsonpath "$.error" == "invalid request"

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
  "unexpected": true
}
HTTP 400

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
  "query": ""
}
HTTP 400

POST {{base_url}}/v1/catalog/search
Authorization: Bearer {{user_token}}
Content-Type: application/json

{
  "retailer_id": "unknown",
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
HTTP 404

POST {{base_url}}/v1/catalog/search
Authorization: Bearer {{user_token}}
Content-Type: application/json

{
  "retailer_id": "ica",
  "context": {
    "store_id": "ica-demo",
    "postal_code": "111 22",
    "fulfillment": "delivery",
    "delivery_slot_id": "demo-slot",
    "memberships": [
      "ica-demo-member"
    ]
  },
  "query": "mjölk"
}
HTTP 400

POST {{base_url}}/v1/catalog/search
Authorization: Bearer {{user_token}}
Content-Type: application/json

{
  "retailer_id": "ica",
  "context": {
    "store_id": "ica-demo",
    "postal_code": "abcde",
    "fulfillment": "delivery",
    "delivery_slot_id": "demo-slot",
    "memberships": [
      "ica-demo-member"
    ]
  },
  "query": "mjölk"
}
HTTP 400

POST {{base_url}}/v1/catalog/search
Authorization: Bearer {{user_token}}
Content-Type: application/json

{
  "retailer_id": "ica",
  "context": {
    "store_id": "",
    "postal_code": "11122",
    "fulfillment": "delivery",
    "delivery_slot_id": "demo-slot",
    "memberships": [
      "ica-demo-member"
    ]
  },
  "query": "mjölk"
}
HTTP 400

POST {{base_url}}/v1/catalog/search
Authorization: Bearer {{user_token}}
Content-Type: application/json

{
  "retailer_id": "ica",
  "context": {
    "store_id": "ica-demo",
    "postal_code": "11122",
    "fulfillment": "teleport",
    "delivery_slot_id": "demo-slot",
    "memberships": [
      "ica-demo-member"
    ]
  },
  "query": "mjölk"
}
HTTP 400

POST {{base_url}}/v1/catalog/search
Authorization: Bearer {{other_token}}
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
HTTP 401

POST {{base_url}}/v1/catalog/search
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
HTTP 401

GET {{base_url}}/v1/catalog/search
Authorization: Bearer {{user_token}}
HTTP 405

POST {{base_url}}/v1/catalog/search
Authorization: Bearer {{user_token}}
Content-Type: text/plain

```
milk
```
HTTP 415

POST {{base_url}}/v1/catalog/search
Authorization: Bearer {{user_token}}
Content-Type: application/json

file,{{oversized_request_file}};
HTTP 413

GET {{io_url}}/catalog/requests
HTTP 200
[Asserts]
jsonpath "$.requests" count == 0
