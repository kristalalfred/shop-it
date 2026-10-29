package backend

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

func DefaultConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "shop-it", "config.json"), nil
}

func EnsureConfig(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	config := Config{
		Listen: "127.0.0.1:24816", APIToken: rand.Text(), AdapterTimeoutMS: 25000,
		DataDir: "data", Interpreter: InterpreterConfig{Mode: "auto"},
		Retailers: []RetailerConfig{
			{ID: "mathem", Name: "Mathem", Mode: "browser", BasketURL: BasketURLs["mathem"]},
			{ID: "willys", Name: "Willys", Mode: "browser", BasketURL: BasketURLs["willys"]},
			{ID: "hemkop", Name: "Hemköp", Mode: "browser", BasketURL: BasketURLs["hemkop"]},
		},
		Clients: []ClientConfig{{ID: "browser", Token: rand.Text()}},
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = file.Write(append(data, '\n'))
	closeErr := file.Close()
	return errors.Join(err, closeErr)
}
