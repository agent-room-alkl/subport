package httpapi

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// spaFileServer serves a Vite/Vue dist directory. Unknown paths without a file
// extension fall back to index.html so client-side routes survive refresh.
func spaFileServer(webDir string) http.Handler {
	root := http.Dir(webDir)
	fileServer := http.FileServer(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upath := path.Clean("/" + r.URL.Path)
		if upath == "/" {
			fileServer.ServeHTTP(w, r)
			return
		}
		fsPath := filepath.Join(webDir, filepath.FromSlash(upath))
		if fi, err := os.Stat(fsPath); err == nil && !fi.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		// Directory with index.html
		if fi, err := os.Stat(fsPath); err == nil && fi.IsDir() {
			idx := filepath.Join(fsPath, "index.html")
			if _, err := os.Stat(idx); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		// Asset-looking paths (have an extension) stay 404.
		base := path.Base(upath)
		if strings.Contains(base, ".") {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(webDir, "index.html"))
	})
}
