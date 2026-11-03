package api

import (
	"bytes"
	"html"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// spaHandler serves the built frontend. The bundle is built with relative
// asset URLs, and index.html gets a <base href="/<secret>/"> injected, so the
// same build works under any secret path. Unknown routes without a file
// extension fall back to index.html for client-side routing.
func spaHandler(files fs.FS, baseHref string) http.Handler {
	index := loadIndex(files, baseHref)
	fileServer := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if p != "" && p != "index.html" {
			if _, err := fs.Stat(files, p); err == nil {
				if strings.HasPrefix(p, "assets/") { // content-hashed by Vite
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
			if path.Ext(p) != "" {
				http.NotFound(w, r)
				return
			}
		}
		if index == nil {
			http.Error(w, "frontend is not built: run `npm run build` in web/", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(index)
	})
}

func loadIndex(files fs.FS, baseHref string) []byte {
	b, err := fs.ReadFile(files, "index.html")
	if err != nil {
		return nil
	}
	tag := []byte(`<head><base href="` + html.EscapeString(baseHref) + `">`)
	return bytes.Replace(b, []byte("<head>"), tag, 1)
}
