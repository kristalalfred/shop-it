package backend

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfigKeepsDataNextToIt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shop-it", "config.json")
	if err := EnsureConfig(path); err != nil {
		t.Fatal(err)
	}
	created, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureConfig(path); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(path); string(again) != string(created) {
		t.Fatal("an existing configuration was replaced")
	}
	t.Chdir(t.TempDir())
	config, err := LoadConfig(filepath.Join("..", filepath.Base(dir), "shop-it", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if config.DataDir != filepath.Join(dir, "shop-it", "data") || config.PluginDir() != filepath.Join(dir, "shop-it", "plugin") {
		t.Fatalf("config: %+v", config)
	}
}
