package mockio

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kristalalfred/ship-it/internal/shopping"
)

type Fault struct {
	Status      int    `json:"status,omitempty"`
	DelayMS     int    `json:"delay_ms,omitempty"`
	BodyDelayMS int    `json:"body_delay_ms,omitempty"`
	Body        string `json:"body,omitempty"`
	BodyRepeat  int    `json:"body_repeat,omitempty"`
}

type CatalogFixture struct {
	Products   []shopping.Product `json:"products"`
	ObservedAt time.Time          `json:"observed_at"`
	PageSize   int                `json:"page_size"`
	Fault      Fault              `json:"fault,omitempty"`
}

type BrowserScenario struct {
	LoginRequired bool           `json:"login_required,omitempty"`
	Unavailable   []string       `json:"unavailable,omitempty"`
	Uncertain     []string       `json:"uncertain,omitempty"`
	Existing      map[string]int `json:"existing,omitempty"`
}

type Scenario struct {
	Retailers map[string]CatalogFixture `json:"retailers"`
	Browser   BrowserScenario           `json:"browser"`
}

func LoadScenario(path string) (Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Scenario{}, err
	}
	var scenario Scenario
	if err := json.Unmarshal(data, &scenario); err != nil {
		return Scenario{}, err
	}
	if len(scenario.Retailers) == 0 {
		return Scenario{}, fmt.Errorf("scenario requires retailers")
	}
	return scenario, nil
}

func RetailerHandler(scenario Scenario) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /retailers/{id}/search", func(w http.ResponseWriter, r *http.Request) {
		fixture, ok := scenario.Retailers[r.PathValue("id")]
		if !ok {
			http.Error(w, "unknown fixture", http.StatusNotFound)
			return
		}
		if fixture.Fault.DelayMS > 0 {
			timer := time.NewTimer(time.Duration(fixture.Fault.DelayMS) * time.Millisecond)
			defer timer.Stop()
			select {
			case <-r.Context().Done():
				return
			case <-timer.C:
			}
		}
		if fixture.Fault.Status != 0 || fixture.Fault.Body != "" || fixture.Fault.BodyDelayMS > 0 {
			status := fixture.Fault.Status
			if status == 0 {
				status = http.StatusOK
			}
			w.WriteHeader(status)
			if fixture.Fault.BodyDelayMS > 0 {
				_ = http.NewResponseController(w).Flush()
				timer := time.NewTimer(time.Duration(fixture.Fault.BodyDelayMS) * time.Millisecond)
				defer timer.Stop()
				select {
				case <-r.Context().Done():
					return
				case <-timer.C:
				}
			}
			body := fixture.Fault.Body
			if fixture.Fault.BodyRepeat > 0 && fixture.Fault.BodyRepeat <= 2<<20 {
				body = strings.Repeat(body, fixture.Fault.BodyRepeat)
			}
			_, _ = w.Write([]byte(body))
			return
		}
		var req shopping.SearchRequest
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req) != nil || req.RetailerID != r.PathValue("id") {
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		products := make([]shopping.Product, 0)
		for _, product := range fixture.Products {
			if strings.Contains(strings.ToLower(product.Name), strings.ToLower(req.Query)) {
				products = append(products, product)
			}
		}
		offset := 0
		if req.Cursor != "" {
			var err error
			offset, err = strconv.Atoi(req.Cursor)
			if err != nil || offset < 0 || offset > len(products) {
				http.Error(w, "invalid cursor", http.StatusBadRequest)
				return
			}
		}
		end := len(products)
		if fixture.PageSize > 0 && offset+fixture.PageSize < end {
			end = offset + fixture.PageSize
		}
		result := shopping.SearchResult{
			RetailerID: req.RetailerID, Context: req.Context, ObservedAt: fixture.ObservedAt,
			Products: products[offset:end],
		}
		if end < len(products) {
			result.NextCursor = strconv.Itoa(end)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	})
	return mux
}
