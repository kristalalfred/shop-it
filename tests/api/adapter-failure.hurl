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
HTTP 502
[Asserts]
jsonpath "$.error" == "adapter unavailable"
body not contains "private upstream error"
