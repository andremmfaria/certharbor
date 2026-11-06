package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"
)

// RequestIDHeader is set by nginx from $request_id on every proxied request,
// overwriting anything the client sent.
const RequestIDHeader = "X-Request-Id"

const ctxKeyRequestID ctxKey = ctxKeyUser + 1

// RequestIDFrom returns the correlation id RequestLog attached, or "".
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(ctxKeyRequestID).(string)
	return id
}

// Logger returns the default logger carrying the request's correlation id.
func Logger(ctx context.Context) *slog.Logger {
	if id := RequestIDFrom(ctx); id != "" {
		return slog.Default().With("request_id", id)
	}
	return slog.Default()
}

// validRequestID bounds what reaches a log line. nginx's $request_id is 32
// hex characters; a direct caller can send anything, so anything else is
// replaced rather than logged.
func validRequestID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, c := range id {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '-':
		default:
			return false
		}
	}
	return true
}

func newRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer's Flush.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// RequestLog takes the correlation id nginx set (or mints one for a direct
// call), echoes it on the response, puts it on the request context and writes
// one structured line per request. It must run after RealIP so the logged
// address is the client rather than the proxy.
func RequestLog() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(RequestIDHeader)
			if !validRequestID(id) {
				id = newRequestID()
			}
			w.Header().Set(RequestIDHeader, id)
			ctx := context.WithValue(r.Context(), ctxKeyRequestID, id)
			rec := &statusRecorder{ResponseWriter: w}
			start := time.Now()
			next.ServeHTTP(rec, r.WithContext(ctx))

			level := slog.LevelInfo
			// Probed every few seconds by healthchecks; at Info they would
			// drown every other line.
			if r.URL.Path == "/health" || r.URL.Path == "/ready" {
				level = slog.LevelDebug
			}
			status := rec.status
			if status == 0 {
				status = http.StatusOK
			}
			slog.Log(ctx, level, "request",
				"request_id", id,
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"duration_ms", time.Since(start).Milliseconds(),
				"client", ClientIP(r),
			)
		})
	}
}
