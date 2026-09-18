package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":         {Data: []byte("<html>index</html>")},
		"favicon.ico":        {Data: []byte("icon")},
		"assets/app-abc.js":  {Data: []byte("console.log('app')")},
		"assets/app-abc.css": {Data: []byte("body{}")},
	}
}

func TestSPAHandler_ServesIndexAtRoot(t *testing.T) {
	h := spaHandler(http.FS(testFS()))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "<html>index</html>" {
		t.Fatalf("body = %q, want index.html contents", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", got)
	}
}

func TestSPAHandler_FallsBackToIndexForUnknownRouteWithoutExtension(t *testing.T) {
	h := spaHandler(http.FS(testFS()))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sessions/abc-123", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "<html>index</html>" {
		t.Fatalf("body = %q, want index.html contents (SPA fallback)", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", got)
	}
}

func TestSPAHandler_UnknownPathWithExtensionIs404(t *testing.T) {
	h := spaHandler(http.FS(testFS()))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/missing.png", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestSPAHandler_AssetsGetImmutableCacheHeader(t *testing.T) {
	h := spaHandler(http.FS(testFS()))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/app-abc.js", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "console.log('app')" {
		t.Fatalf("body = %q, want asset contents", got)
	}
	want := "public, max-age=31536000, immutable"
	if got := rec.Header().Get("Cache-Control"); got != want {
		t.Fatalf("Cache-Control = %q, want %q", got, want)
	}
}

func TestSPAHandler_ExistingFileOutsideAssetsGetsNoCacheHeader(t *testing.T) {
	h := spaHandler(http.FS(testFS()))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "" {
		t.Fatalf("Cache-Control = %q, want unset for a plain existing file", got)
	}
}

func TestSPAHandler_IndexHTMLPathGetsNoCacheHeader(t *testing.T) {
	h := spaHandler(http.FS(testFS()))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", got)
	}
}
