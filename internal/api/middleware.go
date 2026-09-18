package api

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// csrfHeader and csrfHeaderValue implement the CSRF rule: every
// state-changing request (POST/PUT/PATCH/DELETE) must carry this header
// with this exact value, or it is rejected with 403. GET/HEAD/OPTIONS
// (including the OIDC callback, which is a GET) are exempt.
const (
	csrfHeader      = "X-Requested-With"
	csrfHeaderValue = "styr"
)

// csrfGuard enforces the CSRF header rule on non-safe methods.
func csrfGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get(csrfHeader) != csrfHeaderValue {
			writeErrorCode(w, http.StatusForbidden, "forbidden", "missing "+csrfHeader+" header")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// realIP trusts X-Forwarded-For only when the direct peer is loopback (a
// reverse proxy on the same host), mirroring the documented deployment.
func realIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.RemoteAddr
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			fwd := ""
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				parts := strings.Split(xff, ",")
				fwd = strings.TrimSpace(parts[len(parts)-1])
			}
			if fwd != "" && net.ParseIP(fwd) != nil {
				r.RemoteAddr = net.JoinHostPort(fwd, "0")
			}
		}
		next.ServeHTTP(w, r)
	})
}

// securityHeaders adds defence-in-depth response headers.
func securityHeaders(next http.Handler) http.Handler {
	const csp = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data: https:; font-src 'self' data:; connect-src 'self'; " +
		"frame-ancestors 'none'; base-uri 'none'; form-action 'self'; object-src 'none'"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Content-Security-Policy", csp)
		next.ServeHTTP(w, r)
	})
}

// requestLogger logs each request at debug level, skipping the long-lived
// SSE stream.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		if r.URL.Path == "/api/v1/events" {
			return // long-lived; logging it on completion is not useful
		}
		slog.Debug("http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(),
			"bytes", ww.BytesWritten(), "dur", time.Since(start).Round(time.Millisecond), "ip", r.RemoteAddr)
	})
}
