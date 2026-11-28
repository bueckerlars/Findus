package middleware

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
)

const (
	csrfCookieName = "findus_csrf"
	csrfHeaderName = "X-CSRF-Token"
)

// cookieName adds the __Host- prefix behind HTTPS, so that no subdomain or plain-HTTP page can plant the cookie.
func cookieName(base string, secure bool) string {
	if secure {
		return "__Host-" + base
	}
	return base
}

type csrfCtxKeyType struct{}

var csrfCtxKey = csrfCtxKeyType{}

func CSRFToken(ctx context.Context) string {
	v, _ := ctx.Value(csrfCtxKey).(string)
	return v
}

func CSRF(secure bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldSkipCSRF(r) {
				next.ServeHTTP(w, r)
				return
			}
			c, err := r.Cookie(cookieName(csrfCookieName, secure))
			token := ""
			if err != nil || c.Value == "" {
				b := make([]byte, 16)
				_, _ = rand.Read(b)
				token = hex.EncodeToString(b)
				http.SetCookie(w, &http.Cookie{
					Name:     cookieName(csrfCookieName, secure),
					Value:    token,
					Path:     "/",
					MaxAge:   86400 * 30,
					HttpOnly: false,
					Secure:   secure,
					SameSite: http.SameSiteLaxMode,
				})
			} else {
				token = c.Value
			}
			ctx := context.WithValue(r.Context(), csrfCtxKey, token)
			r = r.WithContext(ctx)
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			// Header only: reading the token from a form would parse uploads before any check.
			got := strings.TrimSpace(r.Header.Get(csrfHeaderName))
			if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				http.Error(w, "invalid csrf token", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func shouldSkipCSRF(r *http.Request) bool {
	if strings.HasPrefix(r.URL.Path, "/static/") || strings.HasPrefix(r.URL.Path, "/assets/") {
		return true
	}
	// /mcp authenticates only with the X-API-Key or Authorization header and ignores cookies, so CSRF does not apply.
	if r.URL.Path == "/healthz" || r.URL.Path == "/mcp" {
		return true
	}
	return false
}
