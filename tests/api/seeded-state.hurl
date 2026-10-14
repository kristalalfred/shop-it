GET {{base_url}}/v1/basket-transfers/{{seed_pending_id}}
Authorization: Bearer {{user_token}}
HTTP 200
[Asserts]
jsonpath "$.state" == "pending"
jsonpath "$.items[0].target_quantity" == 2

GET {{base_url}}/v1/basket-transfers/{{seed_claimed_id}}
Authorization: Bearer {{user_token}}
HTTP 200
[Asserts]
jsonpath "$.state" == "claimed"
jsonpath "$.items[0].target_quantity" == 3
jsonpath "$.receipt" not exists

GET {{base_url}}/v1/basket-transfers/{{seed_completed_id}}
Authorization: Bearer {{user_token}}
HTTP 200
[Asserts]
jsonpath "$.state" == "completed"
jsonpath "$.receipt.lines[0].confirmed_quantity" == 1

POST {{base_url}}/v1/client/claims
Authorization: Bearer {{client_token}}
HTTP 200
[Asserts]
jsonpath "$.id" == "{{seed_pending_id}}"

POST {{base_url}}/v1/client/claims
Authorization: Bearer {{client_token}}
HTTP 204
