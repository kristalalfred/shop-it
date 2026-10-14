package backend

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
)

type RetailerConfig struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	GatewayURL string `json:"gateway_url,omitempty"`
	BasketURL  string `json:"basket_url"`
	Mode       string `json:"mode"`
}

type ClientConfig struct {
	ID    string `json:"id"`
	Token string `json:"token"`
}

type Config struct {
	Listen           string            `json:"listen"`
	APIToken         string            `json:"api_token"`
	AdapterTimeoutMS int               `json:"adapter_timeout_ms"`
	Retailers        []RetailerConfig  `json:"retailers"`
	Clients          []ClientConfig    `json:"clients"`
	DataDir          string            `json:"data_dir,omitempty"`
	Interpreter      InterpreterConfig `json:"interpreter,omitempty"`
}

type InterpreterConfig struct {
	Mode  string `json:"mode"`
	Model string `json:"model,omitempty"`
}

func LoadConfig(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()
	var config Config
	d := json.NewDecoder(io.LimitReader(f, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(&config); err != nil {
		return Config{}, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return Config{}, fmt.Errorf("configuration must contain one JSON object")
	}
	return config, config.Validate()
}

func (c Config) Validate() error {
	host, _, err := net.SplitHostPort(c.Listen)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return fmt.Errorf("listen address must be a loopback IP and port")
	}
	if strings.TrimSpace(c.APIToken) == "" || c.AdapterTimeoutMS < 1 || c.AdapterTimeoutMS > 30000 {
		return fmt.Errorf("api_token and adapter_timeout_ms (1..30000) are required")
	}
	if len(c.Retailers) == 0 || len(c.Clients) == 0 {
		return fmt.Errorf("at least one retailer and client are required")
	}
	if c.Interpreter.Mode != "" && c.Interpreter.Mode != "builtin" && c.Interpreter.Mode != "openai" {
		return fmt.Errorf("interpreter mode must be builtin or openai")
	}
	if c.Interpreter.Mode == "openai" && strings.TrimSpace(c.Interpreter.Model) == "" {
		return fmt.Errorf("openai interpreter requires a model")
	}
	ids, tokens := map[string]bool{}, map[string]bool{c.APIToken: true}
	for _, r := range c.Retailers {
		if !validID(r.ID) || ids[r.ID] || strings.TrimSpace(r.Name) == "" || (r.Mode != "mock" && r.Mode != "gateway" && r.Mode != "browser") {
			return fmt.Errorf("retailer IDs must be unique, named, and use mock, gateway or browser mode")
		}
		if !validURL(r.BasketURL) || r.Mode != "browser" && !validURL(r.GatewayURL) {
			return fmt.Errorf("retailer %s requires absolute HTTP gateway and basket URLs", r.ID)
		}
		if r.Mode == "browser" {
			origins := map[string]string{"willys": "https://www.willys.se/", "hemkop": "https://www.hemkop.se/", "mathem": "https://www.mathem.se/se/cart/"}
			if origins[r.ID] != r.BasketURL || r.GatewayURL != "" {
				return fmt.Errorf("retailer %s has no supported browser adapter or an incorrect basket URL", r.ID)
			}
		}
		ids[r.ID] = true
	}
	ids = map[string]bool{}
	for _, client := range c.Clients {
		if !validID(client.ID) || ids[client.ID] || strings.TrimSpace(client.Token) == "" || tokens[client.Token] {
			return fmt.Errorf("client IDs and tokens must be nonempty and unique")
		}
		ids[client.ID], tokens[client.Token] = true, true
	}
	return nil
}

func validID(id string) bool {
	if len(id) < 1 || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:-", r)) {
			return false
		}
	}
	return true
}

func validURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}
