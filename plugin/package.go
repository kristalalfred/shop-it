package plugin

import (
	"archive/zip"
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed assets
var assets embed.FS

func Build(dir, configPath, address string) error {
	path, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	if err := fs.WalkDir(assets, "assets", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := assets.ReadFile(path)
		if err != nil {
			return err
		}
		destination := filepath.Join(dir, path[len("assets/"):])
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			return err
		}
		return os.WriteFile(destination, data, 0644)
	}); err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	// Claude: Codex rejects stdio commands and cwd values outside the plugin root, so the service's working directory travels as -C.
	manifest := map[string]any{"$schema": "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json", "mcpServers": map[string]any{"ship-it": map[string]any{"type": "stdio", "cwd": "${PLUGIN_ROOT}", "command": "./bin/ship-it", "args": []string{"-C", cwd, "-mcp", "-config", path, "-listen", address}}}}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), append(data, '\n'), 0644); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	source, err := os.Open(exe)
	if err != nil {
		return err
	}
	defer source.Close()
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0755); err != nil {
		return err
	}
	destination, err := os.OpenFile(filepath.Join(dir, "bin", "ship-it"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(destination, source)
	closeErr := destination.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func Archive(dir, path string) error {
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
