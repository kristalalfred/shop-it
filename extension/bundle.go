package extension

import (
	"archive/zip"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed assets/*
var assets embed.FS

func files(browser string, write func(string, []byte) error) error {
	if browser != "chromium" && browser != "firefox" {
		return fmt.Errorf("unsupported browser")
	}
	return fs.WalkDir(assets, "assets", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		name := entry.Name()
		if name == "manifest.firefox.json" || name == "manifest.json" && browser == "firefox" {
			return nil
		}
		data, err := assets.ReadFile(path)
		if err != nil {
			return err
		}
		return write(name, data)
	})
}

func writeFiles(browser string, write func(string, []byte) error) error {
	if err := files(browser, write); err != nil {
		return err
	}
	if browser == "firefox" {
		data, err := assets.ReadFile("assets/manifest.firefox.json")
		if err != nil {
			return err
		}
		return write("manifest.json", data)
	}
	return nil
}

func Icon() []byte {
	data, _ := assets.ReadFile("assets/icon-32.png")
	return data
}

func Build(dir string) error {
	for _, browser := range []string{"chromium", "firefox"} {
		destination := filepath.Join(dir, browser)
		if err := os.MkdirAll(destination, 0755); err != nil {
			return err
		}
		if err := writeFiles(browser, func(name string, data []byte) error {
			return os.WriteFile(filepath.Join(destination, name), data, 0644)
		}); err != nil {
			return err
		}
		archive, err := os.Create(destination + ".zip")
		if err != nil {
			return err
		}
		err = Archive(archive, browser)
		if closeErr := archive.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func Archive(w io.Writer, browser string) error {
	archive := zip.NewWriter(w)
	err := writeFiles(browser, func(name string, data []byte) error {
		file, err := archive.Create(name)
		if err != nil {
			return err
		}
		_, err = file.Write(data)
		return err
	})
	closeErr := archive.Close()
	if err != nil {
		return err
	}
	return closeErr
}
