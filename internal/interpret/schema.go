package interpret

func grocerySchema() map[string]any {
	object := func(props map[string]any, required ...string) map[string]any {
		return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
	}
	str := map[string]any{"type": "string"}
	array := func(items any) map[string]any { return map[string]any{"type": "array", "items": items} }
	item := object(map[string]any{
		"label": str, "query": str,
		"amount":          object(map[string]any{"quantity": map[string]any{"type": "integer"}, "unit": map[string]any{"type": "string", "enum": []string{"g", "ml", "each", "pack"}}}, "quantity", "unit"),
		"required_traits": array(map[string]any{"type": "string", "enum": []string{"organic", "vegan", "lactose_free", "gluten_free"}}),
	}, "label", "query", "amount", "required_traits")
	return object(map[string]any{"items": array(item), "warnings": array(str)}, "items", "warnings")
}
