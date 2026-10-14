GET {{base_url}}/healthz
HTTP 200
[Asserts]
jsonpath "$.status" == "ok"
jsonpath "$.mode" == "scaffold"

GET {{base_url}}/v1/retailers
Authorization: Bearer {{user_token}}
HTTP 200
[Asserts]
jsonpath "$.retailers" count == 6
jsonpath "$.retailers[0].id" == "ica"
jsonpath "$.retailers[1].id" == "mathem"
jsonpath "$.retailers[2].id" == "willys"
jsonpath "$.retailers[3].id" == "coop"
jsonpath "$.retailers[4].id" == "hemkop"
jsonpath "$.retailers[5].id" == "citygross"
jsonpath "$.retailers[0].mode" == "mock"
jsonpath "$.retailers[1].mode" == "mock"
jsonpath "$.retailers[2].mode" == "mock"
jsonpath "$.retailers[3].mode" == "mock"
jsonpath "$.retailers[4].mode" == "mock"
jsonpath "$.retailers[5].mode" == "mock"
jsonpath "$.retailers[0].gateway_url" not exists
