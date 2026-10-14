package tests_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kristalalfred/ship-it/internal/backend"
	"github.com/kristalalfred/ship-it/internal/mockio"
	"github.com/kristalalfred/ship-it/internal/shopping"
	"github.com/kristalalfred/ship-it/internal/testenv"
)

const userToken = "local-demo-api-token"
const clientToken = "local-demo-client-token"

var binary string

func TestMain(m *testing.M) {
	if provided := os.Getenv("SHIP_IT_TEST_BINARY"); provided != "" {
		binary = provided
		os.Exit(m.Run())
	}
	dir, err := os.MkdirTemp("", "ship-it-blackbox-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binary = filepath.Join(dir, "ship-it")
	cmd := exec.Command("go", "build", "-race", "-o", binary, "./cmd/ship-it")
	cmd.Dir = ".."
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	code := 1
	if err := cmd.Run(); err == nil {
		code = m.Run()
	} else {
		fmt.Fprintln(os.Stderr, err)
	}
	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	os.Exit(code)
}

type system struct {
	url    string
	client *http.Client
}

func demoScenario(t *testing.T) mockio.Scenario {
	t.Helper()
	scenario, err := mockio.LoadScenario("../testdata/demo.json")
	if err != nil {
		t.Fatal(err)
	}
	return scenario
}

func startSystem(t *testing.T, scenario mockio.Scenario) system {
	t.Helper()
	config, err := backend.LoadConfig("../config/dev.json")
	if err != nil {
		t.Fatal(err)
	}
	env, err := testenv.Start(context.Background(), testenv.Options{Binary: binary, Config: config, Scenario: scenario})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := env.Close(); err != nil {
			t.Error(err)
		}
	})
	return system{url: env.URL, client: env.Client}
}

type response struct {
	status int
	data   map[string]any
}

func (s system) request(method, path string, input any, token, key string) (response, error) {
	var payload []byte
	if input != nil {
		var err error
		payload, err = json.Marshal(input)
		if err != nil {
			return response{}, err
		}
	}
	req, err := http.NewRequest(method, s.url+path, bytes.NewReader(payload))
	if err != nil {
		return response{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	res, err := s.client.Do(req)
	if err != nil {
		return response{}, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return response{}, err
	}
	data := map[string]any{}
	if len(body) > 0 && strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		if err := json.Unmarshal(body, &data); err != nil {
			return response{}, fmt.Errorf("invalid JSON response: %s", body)
		}
	}
	return response{res.StatusCode, data}, nil
}

func storeContext(retailer string) shopping.StoreContext {
	return shopping.StoreContext{StoreID: retailer + "-demo", PostalCode: "11122", Fulfillment: "delivery", DeliverySlotID: "demo-slot", Memberships: []string{retailer + "-demo-member"}}
}

func transferRequest() shopping.TransferRequest {
	return shopping.TransferRequest{
		RetailerID: "ica", ClientID: "demo-browser", Context: storeContext("ica"),
		Items: []shopping.BasketItem{{ProductID: "milk-basic", Quantity: 2}, {ProductID: "milk-lactose-free", Quantity: 1}},
	}
}

func TestConcurrentIdempotencyAndClaims(t *testing.T) {
	s := startSystem(t, demoScenario(t))
	var wg sync.WaitGroup
	ids := make(chan string, 12)
	for range 12 {
		wg.Go(func() {
			created, err := s.request("POST", "/v1/basket-transfers", transferRequest(), userToken, "concurrent")
			if err != nil {
				t.Error(err)
				return
			}
			if created.status != 202 && created.status != 200 {
				t.Errorf("unexpected create status: %d", created.status)
				return
			}
			id, ok := created.data["id"].(string)
			if !ok {
				t.Error("transfer response omitted its ID")
				return
			}
			ids <- id
		})
	}
	wg.Wait()
	close(ids)
	var id string
	count := 0
	for got := range ids {
		count++
		if id != "" && got != id {
			t.Fatal("concurrent retry created duplicate tasks")
		}
		id = got
	}
	if count != 12 {
		t.Fatalf("only %d create requests succeeded", count)
	}
	claims := make(chan int, 12)
	for range 12 {
		wg.Go(func() {
			res, err := s.request("POST", "/v1/client/claims", nil, clientToken, "")
			if err != nil {
				t.Error(err)
				return
			}
			claims <- res.status
		})
	}
	wg.Wait()
	close(claims)
	claimed := 0
	count = 0
	for status := range claims {
		count++
		if status == 200 {
			claimed++
		} else if status != 204 {
			t.Fatalf("unexpected claim status: %d", status)
		}
	}
	if count != 12 || claimed != 1 {
		t.Fatalf("%d claim requests completed; transfer delivered %d times", count, claimed)
	}
	stored, err := s.request("GET", "/v1/basket-transfers/"+id, nil, userToken, "")
	if err != nil {
		t.Fatal(err)
	}
	if stored.status != 200 || stored.data["state"] != "claimed" {
		t.Fatal(stored.data)
	}
}
