package backend

import (
	"embed"
	"io/fs"
	"net/http"
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
		http.FileServer(http.FS(files)).ServeHTTP(w, r)
	})
	mux.Handle("GET /{$}", handler)
	mux.Handle("GET /app.js", handler)
	mux.Handle("GET /app.css", handler)
}
