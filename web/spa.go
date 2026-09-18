package web

import (
	"net/http"
	"path"
	"strings"
)

// spaHandler serves files from fsys. Paths under /assets/ get an immutable,
// long-lived cache header. Paths that do not exist and have no file
// extension are treated as client-side routes and fall back to
// index.html (served with a no-cache header); paths with a file extension
// that do not exist are served (and 404) as-is.
func spaHandler(fsys http.FileSystem) http.Handler {
	fileServer := http.FileServer(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upath := r.URL.Path
		if !strings.HasPrefix(upath, "/") {
			upath = "/" + upath
		}
		upath = path.Clean(upath)

		if strings.HasPrefix(upath, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			fileServer.ServeHTTP(w, r)
			return
		}

		if upath == "/" {
			w.Header().Set("Cache-Control", "no-cache")
			fileServer.ServeHTTP(w, r)
			return
		}
		if upath == "/index.html" {
			// http.FileServer 301-redirects an exact "/index.html" request
			// to "/"; rewrite it ourselves so it serves directly instead.
			w.Header().Set("Cache-Control", "no-cache")
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}

		if f, err := fsys.Open(upath); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		if path.Ext(upath) == "" {
			w.Header().Set("Cache-Control", "no-cache")
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}

		fileServer.ServeHTTP(w, r)
	})
}
