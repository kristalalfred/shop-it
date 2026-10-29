package retailer

import (
	"bytes"
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/kristalalfred/shop-it/internal/shopping"
)

func ParseStorefront(retailerID string, data []byte, page int) ([]shopping.Product, bool, error) {
	if retailerID == "mathem" {
		return parseMathem(data, page)
	}
	return parseAxfood(data, page)
}

type flexible string

func (f *flexible) UnmarshalJSON(data []byte) error {
	var text string
	if json.Unmarshal(data, &text) == nil {
		*f = flexible(text)
		return nil
	}
	if bytes.Equal(data, []byte("null")) {
		*f = ""
		return nil
	}
	*f = flexible(data)
	return nil
}

func (f flexible) number() (float64, bool) {
	text := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(string(f)), " ", ""), ",", ".")
	value, err := strconv.ParseFloat(text, 64)
	return value, err == nil && !math.IsInf(value, 0) && !math.IsNaN(value)
}

func price(value float64) (shopping.Money, bool) {
	if value < 0 || value > 1000000 {
		return shopping.Money{}, false
	}
	return shopping.Money{AmountMinor: int64(math.Round(value * 100)), Currency: "SEK"}, true
}

var (
	perKilogram = regexp.MustCompile(`/kg\b`)
	approximate = regexp.MustCompile(`(?i)(?:ca\.?|cirka|approx)`)
	packSize    = regexp.MustCompile(`(?:([0-9]+)\s*[x×]\s*)?([0-9]+(?:[.,][0-9]+)?)\s*(kg|g|ml|cl|dl|l|st|stycken|pcs|pack|p)(?:\b|$)`)
)

func pack(text string) (shopping.Pack, bool) {
	match := packSize.FindStringSubmatch(strings.ToLower(text))
	if match == nil {
		return shopping.Pack{}, false
	}
	units := map[string]struct {
		unit   string
		factor float64
	}{"kg": {"g", 1000}, "g": {"g", 1}, "l": {"ml", 1000}, "dl": {"ml", 100}, "cl": {"ml", 10}, "ml": {"ml", 1}, "st": {"each", 1}, "stycken": {"each", 1}, "pcs": {"each", 1}, "pack": {"each", 1}, "p": {"each", 1}}
	count := 1.0
	if match[1] != "" {
		count, _ = strconv.ParseFloat(match[1], 64)
	}
	amount, ok := flexible(match[2]).number()
	if !ok {
		return shopping.Pack{}, false
	}
	unit := units[match[3]]
	quantity := int64(math.Round(count * amount * unit.factor))
	if quantity < 1 || quantity > 1000000000 {
		return shopping.Pack{}, false
	}
	return shopping.Pack{Quantity: quantity, Unit: unit.unit}, true
}

func traits(labels []string, name string) map[string]string {
	evidence := map[string]bool{}
	for _, label := range labels {
		evidence[strings.ToLower(label)] = true
	}
	for _, word := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool { return !unicode.IsLetter(r) }) {
		evidence[word] = true
	}
	aliases := map[string][]string{
		"organic":      {"ecological", "krav", "eu_ecological", "organic", "eko", "ekologisk", "ekologiskt", "ekologiska"},
		"vegan":        {"vegan", "vegansk", "veganskt", "veganska", "växtbaserad", "växtbaserat", "växtbaserade"},
		"lactose_free": {"lactose_free", "lactose free", "laktosfri", "laktosfritt", "laktosfria"},
		"gluten_free":  {"gluten_free", "gluten free", "glutenfri", "glutenfritt", "glutenfria"},
	}
	result := map[string]string{}
	for trait, words := range aliases {
		result[trait] = "unknown"
		for _, word := range words {
			if evidence[word] {
				result[trait] = "yes"
			}
		}
	}
	if result["vegan"] == "yes" {
		result["lactose_free"] = "yes"
	}
	return result
}

type axfoodProduct struct {
	Code                       string   `json:"code"`
	Name                       string   `json:"name"`
	Manufacturer               string   `json:"manufacturer"`
	DisplayVolume              string   `json:"displayVolume"`
	PriceValue                 flexible `json:"priceValue"`
	PriceUnit                  string   `json:"priceUnit"`
	AverageWeight              flexible `json:"averageWeight"`
	Labels                     []string `json:"labels"`
	Online                     bool     `json:"online"`
	OutOfStock                 bool     `json:"outOfStock"`
	AddToCartDisabled          bool     `json:"addToCartDisabled"`
	TobaccoProduct             bool     `json:"tobaccoProduct"`
	TobaccoFreeNicotineProduct bool     `json:"tobaccoFreeNicotineProduct"`
	NicotineMedicalProduct     bool     `json:"nicotineMedicalProduct"`
	IsDrugProduct              bool     `json:"isDrugProduct"`
	ProductBasketType          struct {
		Code string `json:"code"`
	} `json:"productBasketType"`
}

func parseAxfood(body []byte, page int) ([]shopping.Product, bool, error) {
	var data struct {
		Results    []axfoodProduct `json:"results"`
		Pagination *struct {
			NumberOfPages int `json:"numberOfPages"`
		} `json:"pagination"`
	}
	if json.Unmarshal(body, &data) != nil || data.Pagination == nil {
		return nil, false, shopping.ErrUpstream
	}
	products := []shopping.Product{}
	seen := map[string]bool{}
	for _, raw := range data.Results {
		if product, ok := axfood(raw); ok && !seen[product.ID] {
			seen[product.ID] = true
			products = append(products, product)
		}
	}
	return products, page+1 < data.Pagination.NumberOfPages, nil
}

func axfood(raw axfoodProduct) (shopping.Product, bool) {
	if raw.TobaccoProduct || raw.TobaccoFreeNicotineProduct || raw.NicotineMedicalProduct || raw.IsDrugProduct || raw.Code == "" || strings.TrimSpace(raw.Name) == "" {
		return shopping.Product{}, false
	}
	byWeight := raw.ProductBasketType.Code == "KG"
	size, ok := shopping.Pack{Quantity: 1000, Unit: "g"}, true
	if !byWeight {
		size, ok = pack(raw.DisplayVolume)
	}
	value, valid := raw.PriceValue.number()
	if !ok || !valid {
		return shopping.Product{}, false
	}
	weightPrice := perKilogram.MatchString(raw.PriceUnit)
	if weightPrice && !byWeight {
		weight, ok := raw.AverageWeight.number()
		if !ok || weight <= 0 {
			return shopping.Product{}, false
		}
		value *= weight
	}
	money, ok := price(value)
	if !ok {
		return shopping.Product{}, false
	}
	product := shopping.Product{ID: raw.Code, Name: raw.Name, Brand: raw.Manufacturer, Pack: size, Price: money, PriceKind: "fixed", Availability: "unknown", Traits: traits(raw.Labels, raw.Name)}
	if weightPrice || byWeight {
		product.PriceKind = "estimated"
	}
	if raw.OutOfStock || raw.AddToCartDisabled {
		product.Availability = "out_of_stock"
	} else if raw.Online {
		product.Availability = "in_stock"
	}
	return product, true
}

type mathemProduct struct {
	ID           flexible `json:"id"`
	FullName     string   `json:"full_name"`
	Brand        string   `json:"brand"`
	NameExtra    string   `json:"name_extra"`
	GrossPrice   flexible `json:"gross_price"`
	Currency     string   `json:"currency"`
	Availability *struct {
		IsAvailable *bool `json:"is_available"`
	} `json:"availability"`
	ClientClassifiers []struct {
		Name string `json:"name"`
	} `json:"client_classifiers"`
}

func parseMathem(body []byte, page int) ([]shopping.Product, bool, error) {
	var data struct {
		Products   []mathemProduct `json:"products"`
		Attributes struct {
			TotalHits int `json:"total_hits"`
		} `json:"attributes"`
	}
	if json.Unmarshal(body, &data) != nil || data.Products == nil {
		return nil, false, shopping.ErrUpstream
	}
	products := []shopping.Product{}
	seen := map[string]bool{}
	for _, raw := range data.Products {
		if product, ok := mathem(raw); ok && !seen[product.ID] {
			seen[product.ID] = true
			products = append(products, product)
		}
	}
	return products, (page+1)*40 < data.Attributes.TotalHits, nil
}

func mathem(raw mathemProduct) (shopping.Product, bool) {
	size, ok := pack(raw.NameExtra)
	value, valid := raw.GrossPrice.number()
	if !ok || !valid || raw.ID == "" || strings.TrimSpace(raw.FullName) == "" || raw.Currency != "SEK" {
		return shopping.Product{}, false
	}
	money, ok := price(value)
	if !ok {
		return shopping.Product{}, false
	}
	labels := make([]string, len(raw.ClientClassifiers))
	for i, classifier := range raw.ClientClassifiers {
		labels[i] = classifier.Name
	}
	product := shopping.Product{ID: string(raw.ID), Name: raw.FullName, Brand: raw.Brand, Pack: size, Price: money, PriceKind: "fixed", Availability: "unknown", Traits: traits(labels, raw.FullName)}
	if approximate.MatchString(raw.NameExtra) {
		product.PriceKind = "estimated"
	}
	if raw.Availability != nil && raw.Availability.IsAvailable != nil {
		product.Availability = map[bool]string{true: "in_stock", false: "out_of_stock"}[*raw.Availability.IsAvailable]
	}
	return product, true
}
