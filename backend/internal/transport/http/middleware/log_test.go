package middleware

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestLog_ErrorCarriesDiagnostics(t *testing.T) {
	var out bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&out, nil))
	h := RequestLog(log, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		http.Error(w, "database is locked"+strings.Repeat("x", 1000), http.StatusBadRequest)
	}))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/items/a", strings.NewReader("12345")))

	got := out.String()
	for _, want := range []string{`"body_bytes":5`, `"body_read_ms":`, `"db_pool_wait_ms":0`, `"client_gone":false`, `"error":"database is locked`} {
		if !strings.Contains(got, want) {
			t.Errorf("log lacks %s: %s", want, got)
		}
	}
	if strings.Contains(got, strings.Repeat("x", maxLoggedErrorBytes)) {
		t.Errorf("error not truncated: %s", got)
	}
	if rr.Body.Len() < 1000 {
		t.Errorf("response was truncated: %d bytes", rr.Body.Len())
	}
}
