package middleware

import (
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const slowRequestThreshold = 2 * time.Second

func skipAccessLog(path string) bool {
	switch {
	case path == "/healthz":
		return true
	case strings.HasPrefix(path, "/static/"):
		return true
	case strings.HasPrefix(path, "/assets/"):
		return true
	default:
		return false
	}
}

// RequestLog emits structured access records: skips health and static assets;
// successful fast requests log at Debug; client errors log at Warn, server errors at Error, slow requests at Info.
// Errors and slow requests carry diagnostics: time spent reading the body and waiting for a database connection.
func RequestLog(log *slog.Logger, db *sql.DB) func(http.Handler) http.Handler {
	poolStats := func() sql.DBStats {
		if db == nil {
			return sql.DBStats{}
		}
		return db.Stats()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if skipAccessLog(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			start := time.Now()
			before := poolStats()
			body := &timedBody{ReadCloser: r.Body}
			r.Body = body
			wrapped := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(wrapped, r)
			dur := time.Since(start)
			attrs := []any{
				slog.String("event", "http_access"),
				slog.String("http.method", r.Method),
				slog.String("http.path", r.URL.Path),
				slog.Int("http.status_code", wrapped.status),
				slog.Int64("duration_ms", dur.Milliseconds()),
			}
			if wrapped.status >= 400 || dur >= slowRequestThreshold {
				after := poolStats()
				attrs = append(attrs,
					slog.Int64("body_bytes", body.bytes),
					slog.Int64("body_read_ms", body.spent.Milliseconds()),
					// Pool-wide: waits of all requests that ended while this one ran, so it can exceed duration_ms.
					slog.Int64("db_pool_wait_ms", (after.WaitDuration-before.WaitDuration).Milliseconds()),
					slog.Int64("db_pool_wait_count", after.WaitCount-before.WaitCount),
					slog.Bool("client_gone", r.Context().Err() != nil),
				)
				if len(wrapped.errBody) > 0 {
					attrs = append(attrs, slog.String("error", strings.TrimSpace(string(wrapped.errBody))))
				}
			}
			switch {
			case wrapped.status >= 500:
				log.Error("http request failed", attrs...)
			case wrapped.status >= 400:
				log.Warn("http client error", attrs...)
			case dur >= slowRequestThreshold:
				log.Info("http slow request", append(attrs, slog.Bool("slow", true))...)
			default:
				log.Debug("http request", attrs...)
			}
		})
	}
}

// timedBody measures how long the handler waits for the request body, which exposes slow uploads.
type timedBody struct {
	io.ReadCloser
	bytes int64
	spent time.Duration
}

func (b *timedBody) Read(p []byte) (int, error) {
	start := time.Now()
	n, err := b.ReadCloser.Read(p)
	b.spent += time.Since(start)
	b.bytes += int64(n)
	return n, err
}

const maxLoggedErrorBytes = 300

type statusWriter struct {
	http.ResponseWriter
	status  int
	errBody []byte // start of an error response, for the log
}

func (s *statusWriter) Write(p []byte) (int, error) {
	if s.status >= 400 {
		s.errBody = append(s.errBody, p[:min(len(p), maxLoggedErrorBytes-len(s.errBody))]...)
	}
	return s.ResponseWriter.Write(p)
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
