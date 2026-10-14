package backend

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

func InitLocal(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	config := Config{
		Listen: "127.0.0.1:18080", APIToken: rand.Text(), AdapterTimeoutMS: 25000,
		DataDir: filepath.Join(filepath.Dir(path), "live"), Interpreter: InterpreterConfig{Mode: "builtin"},
		Retailers: []RetailerConfig{
			{ID: "mathem", Name: "Mathem", Mode: "browser", BasketURL: "https://www.mathem.se/se/cart/"},
			{ID: "willys", Name: "Willys", Mode: "browser", BasketURL: "https://www.willys.se/"},
			{ID: "hemkop", Name: "Hemköp", Mode: "browser", BasketURL: "https://www.hemkop.se/"},
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
