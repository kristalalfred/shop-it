package planner

import "testing"

func TestWithoutTraitWords(t *testing.T) {
	for _, tc := range []struct {
		query  string
		traits []string
		want   string
	}{
		{"Ekologisk glutenfri falukorv", []string{"organic", "gluten_free"}, "falukorv"},
		{"ekologisk laktosfri matlagningsgrädde", []string{"organic"}, "laktosfri matlagningsgrädde"},
		{"ekologiskt ris", nil, "ekologiskt ris"},
		{"vegansk korv", []string{"vegan"}, "korv"},
	} {
		if got := WithoutTraitWords(tc.query, tc.traits); got != tc.want {
			t.Errorf("%q without %v: %q", tc.query, tc.traits, got)
		}
	}
}
