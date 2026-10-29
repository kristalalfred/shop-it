package interpret

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/kristalalfred/shop-it/internal/shopping"
)

type Builtin struct{}

var separators = regexp.MustCompile(`(?i)\s+(?:and|och|plus)\s+|[\n;]+`)
var bullets = regexp.MustCompile(`^\s*(?:[-*•]\s*|\d+[.)]\s+)`)
var intro = regexp.MustCompile(`^(?:(?:i|we)\s+(?:need|want|would like)|please\s+(?:buy|get|add)|(?:buy|get|add)|(?:jag|vi)\s+(?:behöver|vill ha)|köp|handla)\s+`)
var ending = regexp.MustCompile(`\s+(?:please|tack|for tomorrow|till imorgon)[.!]*$`)
var quantity = regexp.MustCompile(`(?i)(?:^|\s)([0-9]+(?:[.,][0-9]+)?)\s*(kilograms?|kilos?|kg|grams?|gram|g|litres?|liters?|liter|litre|l|millilitres?|milliliters?|ml|deciliters?|dl|packs?|packets?|packages?|paket|st|stycken|pcs|each|x)?(?:\s+of)?\b`)
var decimalComma = regexp.MustCompile(`([0-9]),([0-9])`)
var compactUnit = regexp.MustCompile(`([0-9])([a-zA-Z]+)\b`)
var sentences = regexp.MustCompile(`([^0-9])[.!?](?:\s+|$)`)

var traitNames = []string{"organic", "vegan", "lactose_free", "gluten_free"}

var traitWords = map[string]*regexp.Regexp{
	"organic":      regexp.MustCompile(`(?i)\b(?:organic|ekologisk|ekologiskt|ekologiska|eko)\b`),
	"vegan":        regexp.MustCompile(`(?i)\b(?:vegan|vegansk|veganskt|veganska|plant-based)\b`),
	"lactose_free": regexp.MustCompile(`(?i)\b(?:lactose-free|lactose free|laktosfri|laktosfritt|laktosfria)\b`),
	"gluten_free":  regexp.MustCompile(`(?i)\b(?:gluten-free|gluten free|glutenfri|glutenfritt|glutenfria)\b`),
}

var aliases = map[string]string{
	"milk": "mjölk", "mjölk": "mjölk", "bananas": "bananer", "banana": "bananer", "banan": "bananer", "bananer": "bananer",
	"oat milk": "havredryck", "oat drink": "havredryck", "havremjölk": "havredryck", "havredryck": "havredryck",
	"eggs": "ägg", "egg": "ägg", "ägg": "ägg", "bread": "bröd", "bröd": "bröd", "pasta": "pasta",
	"tomatoes": "tomater", "tomato": "tomater", "tomater": "tomater", "rice": "ris", "ris": "ris", "coffee": "kaffe", "kaffe": "kaffe",
	"apples": "äpplen", "apple": "äpplen", "äpplen": "äpplen", "potatoes": "potatis", "potatis": "potatis",
	"chicken": "kyckling", "kyckling": "kyckling", "butter": "smör", "smör": "smör", "cheese": "ost", "ost": "ost",
	"yogurt": "yoghurt", "yoghurt": "yoghurt", "onions": "lök", "onion": "lök", "lök": "lök",
}

func (Builtin) Interpret(ctx context.Context, req shopping.InterpretationRequest, _ shopping.Notify) (shopping.Interpretation, error) {
	if err := ctx.Err(); err != nil {
		return shopping.Interpretation{}, err
	}
	result := shopping.Interpretation{Mode: "builtin", Items: []shopping.Item{}, Warnings: []string{}}
	text := req.Text
	var parts []string
	if strings.HasPrefix(strings.TrimSpace(text), "[") {
		if err := json.Unmarshal([]byte(text), &parts); err != nil {
			return result, shopping.ErrInvalid
		}
	} else {
		text = decimalComma.ReplaceAllString(text, "$1.$2")
		text = strings.ReplaceAll(text, ",", ";")
		text = compactUnit.ReplaceAllString(text, "$1 $2")
		text = sentences.ReplaceAllString(text, "$1\n")
		parts = separators.Split(text, -1)
	}
	global := []string{}
	for _, part := range parts {
		if len(result.Items) >= 100 {
			return result, shopping.ErrInvalid
		}
		part = strings.TrimSpace(bullets.ReplaceAllString(part, ""))
		if part == "" {
			continue
		}
		label := part
		part = strings.ToLower(part)
		part = strings.TrimSpace(ending.ReplaceAllString(intro.ReplaceAllString(part, ""), ""))
		traits := []string{}
		for _, trait := range traitNames {
			if traitWords[trait].MatchString(part) {
				traits = append(traits, trait)
				part = traitWords[trait].ReplaceAllString(part, " ")
			}
		}
		if strings.HasPrefix(part, "everything ") || strings.HasPrefix(part, "all items ") || strings.HasPrefix(part, "allt ") {
			if len(traits) == 0 {
				result.Warnings = append(result.Warnings, "Could not interpret: "+label)
			} else {
				global = append(global, traits...)
			}
			continue
		}
		words := strings.Fields(part)
		for i, word := range words {
			if number, ok := map[string]string{"a": "1", "an": "1", "one": "1", "two": "2", "three": "3", "four": "4", "five": "5", "six": "6", "en": "1", "ett": "1", "två": "2", "tre": "3", "fyra": "4", "fem": "5", "sex": "6"}[word]; ok {
				words[i] = number
			}
		}
		part = strings.Join(words, " ")
		amount := shopping.Pack{Quantity: 1, Unit: "pack"}
		matches := quantity.FindAllStringSubmatchIndex(part, -1)
		if len(matches) > 1 {
			result.Warnings = append(result.Warnings, "Check multiple quantities in: "+label)
			if len(part) > 200 {
				return result, shopping.ErrInvalid
			}
			result.Items = append(result.Items, shopping.Item{Label: label, Query: part, Amount: amount, RequiredTraits: traits})
			continue
		}
		if len(matches) == 1 {
			m := matches[0]
			value, _ := strconv.ParseFloat(strings.ReplaceAll(part[m[2]:m[3]], ",", "."), 64)
			unit := ""
			if m[4] >= 0 {
				unit = part[m[4]:m[5]]
			}
			factor := float64(1)
			switch {
			case unit == "kg" || strings.HasPrefix(unit, "kilo"):
				amount.Unit, factor = "g", 1000
			case unit == "g" || strings.HasPrefix(unit, "gram"):
				amount.Unit = "g"
			case unit == "l" || strings.HasPrefix(unit, "lit"):
				amount.Unit, factor = "ml", 1000
			case unit == "dl" || strings.HasPrefix(unit, "deci"):
				amount.Unit, factor = "ml", 100
			case unit == "ml" || strings.HasPrefix(unit, "milli"):
				amount.Unit = "ml"
			case unit == "st" || unit == "stycken" || unit == "pcs" || unit == "each":
				amount.Unit = "each"
			}
			if math.IsInf(value, 0) || math.Abs(value*factor-math.Round(value*factor)) > 0.000001 || value*factor > 1000000 {
				return result, shopping.ErrInvalid
			}
			amount.Quantity = int64(math.Round(value * factor))
			part = part[:m[0]] + " " + part[m[1]:]
		} else {
			result.Warnings = append(result.Warnings, "Assumed one pack for: "+label)
		}
		query := strings.Join(strings.Fields(strings.Trim(part, " .!?")), " ")
		if alias, ok := aliases[query]; ok {
			query = alias
		}
		if len(matches) == 1 && matches[0][4] < 0 && slices.Contains([]string{"bananer", "äpplen", "ägg", "tomater", "lök", "potatis"}, query) {
			amount.Unit = "each"
		}
		item := shopping.Item{Label: label, Query: query, Amount: amount, RequiredTraits: traits}
		if !ValidItem(item) {
			return result, shopping.ErrInvalid
		}
		result.Items = append(result.Items, item)
	}
	for i := range result.Items {
		result.Items[i].RequiredTraits = Unique(append(result.Items[i].RequiredTraits, global...))
	}
	return result, nil
}

func WithoutTraitWords(query string, traits []string) string {
	for _, trait := range traits {
		if words, ok := traitWords[trait]; ok {
			query = words.ReplaceAllString(query, " ")
		}
	}
	return strings.Join(strings.Fields(query), " ")
}

func Unique(traits []string) []string {
	traits = slices.Clone(traits)
	slices.Sort(traits)
	return slices.Compact(traits)
}

func ValidTraits(traits []string) bool {
	if len(traits) > 4 {
		return false
	}
	for _, trait := range traits {
		if _, ok := traitWords[trait]; !ok {
			return false
		}
	}
	return len(Unique(traits)) == len(traits)
}

func ValidItem(item shopping.Item) bool {
	return strings.TrimSpace(item.Label) != "" && len(item.Label) <= 500 && strings.TrimSpace(item.Query) != "" && len(item.Query) <= 200 && item.Amount.Quantity > 0 && item.Amount.Quantity <= 1000000 && (item.Amount.Unit == "g" || item.Amount.Unit == "ml" || (item.Amount.Unit == "each" || item.Amount.Unit == "pack") && item.Amount.Quantity <= 1000) && ValidTraits(item.RequiredTraits)
}

func Validate(result shopping.Interpretation) error {
	if len(result.Items) > 100 || len(result.Warnings) > 100 {
		return shopping.ErrUpstream
	}
	for _, item := range result.Items {
		if !ValidItem(item) {
			return fmt.Errorf("%w: invalid interpreted item", shopping.ErrUpstream)
		}
	}
	for _, warning := range result.Warnings {
		if len(warning) > 1000 {
			return shopping.ErrUpstream
		}
	}
	return nil
}
