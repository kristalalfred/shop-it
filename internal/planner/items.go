package planner

import (
	"regexp"
	"slices"
	"strings"

	"github.com/kristalalfred/shop-it/internal/shopping"
)

var traitWords = map[string]*regexp.Regexp{
	"organic":      regexp.MustCompile(`(?i)\b(?:organic|ekologisk|ekologiskt|ekologiska|eko)\b`),
	"vegan":        regexp.MustCompile(`(?i)\b(?:vegan|vegansk|veganskt|veganska|plant-based)\b`),
	"lactose_free": regexp.MustCompile(`(?i)\b(?:lactose-free|lactose free|laktosfri|laktosfritt|laktosfria)\b`),
	"gluten_free":  regexp.MustCompile(`(?i)\b(?:gluten-free|gluten free|glutenfri|glutenfritt|glutenfria)\b`),
}

func WithoutTraitWords(query string, traits []string) string {
	for _, trait := range traits {
		if words, ok := traitWords[trait]; ok {
			query = words.ReplaceAllString(query, " ")
		}
	}
	return strings.Join(strings.Fields(query), " ")
}

func validTraits(traits []string) bool {
	if len(traits) > 4 {
		return false
	}
	for _, trait := range traits {
		if _, ok := traitWords[trait]; !ok {
			return false
		}
	}
	unique := slices.Clone(traits)
	slices.Sort(unique)
	return len(slices.Compact(unique)) == len(traits)
}

func ValidItem(item shopping.Item) bool {
	return strings.TrimSpace(item.Label) != "" && len(item.Label) <= 500 && strings.TrimSpace(item.Query) != "" && len(item.Query) <= 200 && item.Amount.Quantity > 0 && item.Amount.Quantity <= 1000000 && (item.Amount.Unit == "g" || item.Amount.Unit == "ml" || (item.Amount.Unit == "each" || item.Amount.Unit == "pack") && item.Amount.Quantity <= 1000) && validTraits(item.RequiredTraits)
}
