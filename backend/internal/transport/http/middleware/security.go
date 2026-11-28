package middleware

import (
	"net/http"
	"strings"
)

const (
	maxBodyBytes       = 1 << 20  // JSON requests, and everything sent without a session
	maxUploadBodyBytes = 40 << 20 // signed-in uploads: 32 MiB attachment or import plus form overhead
)

// LimitBody caps request bodies and removes multipart temp files. It must wrap the router directly:
// net/http only cleans up the request it created, not the copies the outer middleware hands down.
func LimitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /mcp sets its own limit after checking the token, before it reads the body.
		if r.URL.Path != "/mcp" {
			limit := int64(maxBodyBytes)
			if _, ok := User(r.Context()); ok &&
				(strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") || r.URL.Path == "/api/admin/inventory-import") {
				limit = maxUploadBodyBytes
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		defer func() {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// contentSecurityPolicy: inline styles are needed for Vue style attributes, blob: for photo
// previews and the label PDF viewer.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; media-src 'self' blob:; font-src 'self'; connect-src 'self'; frame-src blob:; " +
	"object-src blob:; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

// SecurityHeaders sets browser hardening headers on every response. hsts should be true behind HTTPS.
func SecurityHeaders(hsts bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", contentSecurityPolicy)
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "same-origin") // invite links carry their token in the URL
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			h.Set("Permissions-Policy", "camera=(self), microphone=(), geolocation=()")
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=31536000")
			}
			if strings.HasPrefix(r.URL.Path, "/api/") {
				h.Set("Cache-Control", "no-store")
			}
			next.ServeHTTP(w, r)
		})
	}
}
