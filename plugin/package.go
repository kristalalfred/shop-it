package plugin

import (
	"archive/zip"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed assets
var assets embed.FS

//go:embed marketplace.json
var marketplace []byte

func Build(root, configPath string) error {
	configPath, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	dir := filepath.Join(root, "shop-it")
	if err := fs.WalkDir(assets, "assets", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := assets.ReadFile(path)
		if err != nil {
			return err
		}
		return write(filepath.Join(dir, path[len("assets/"):]), data, 0644)
	}); err != nil {
		return err
	}
	// Claude: Codex rejects stdio commands and cwd values outside the plugin root, so the package carries its own copy of the binary.
	manifest := map[string]any{"$schema": "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json", "mcpServers": map[string]any{"shop-it": map[string]any{"type": "stdio", "cwd": "${PLUGIN_ROOT}", "command": "./bin/shop-it", "args": []string{"-mcp", "-config", configPath}}}}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := write(filepath.Join(dir, "mcp.json"), append(data, '\n'), 0644); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	binary, err := os.ReadFile(exe)
	if err != nil {
		return err
	}
	if err := write(filepath.Join(dir, "bin", "shop-it"), binary, 0755); err != nil {
		return err
	}
	if err := write(MarketplacePath(root), marketplace, 0644); err != nil {
		return err
	}
	return archive(dir, filepath.Join(root, "shop-it.zip"))
}

func MarketplacePath(root string) string {
	return filepath.Join(root, ".agents", "plugins", "marketplace.json")
}

func write(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".shop-it-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr, os.Chmod(file.Name(), mode)); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func archive(dir, path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	writer := zip.NewWriter(file)
	walkErr := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		name, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(name)
		header.Method = zip.Deflate
		out, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = io.Copy(out, in)
		return err
	})
	zipErr := writer.Close()
	closeErr := file.Close()
	if walkErr != nil {
		return walkErr
	}
	if zipErr != nil {
		return zipErr
	}
	return closeErr
}
