package backend

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"

	"github.com/kristalalfred/shop-it/extension"
)

//go:embed web/*
var webFiles embed.FS

func (s *Server) registerWeb(mux *http.ServeMux) {
	files, _ := fs.Sub(webFiles, "web")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.URL.Path == "/" {
			page, err := template.ParseFS(webFiles, "web/index.html")
			if err != nil {
				http.Error(w, "page unavailable", 500)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_ = page.Execute(w, s.config)
			return
		}
		http.FileServer(http.FS(files)).ServeHTTP(w, r)
	})
	mux.Handle("GET /{$}", handler)
	mux.Handle("GET /app.js", handler)
	mux.Handle("GET /app.css", handler)
	mux.HandleFunc("GET /extension/{browser}", func(w http.ResponseWriter, r *http.Request) {
		browser := r.PathValue("browser")
		if browser != "chromium.zip" && browser != "firefox.zip" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", "attachment; filename=shop-it-"+browser)
		w.Header().Set("Cache-Control", "no-store")
		_ = extension.Archive(w, browser[:len(browser)-4])
	})
}
