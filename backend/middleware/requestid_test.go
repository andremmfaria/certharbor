package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// captureLog swaps the default logger for one writing JSON into a buffer.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func requestLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		if m["msg"] == "request" {
			out = append(out, m)
		}
	}
	return out
}

// TestRequestLog_PropagatesProxyID: the id nginx set is the one in the
// handler's context, the response header and the structured log line.
func TestRequestLog_PropagatesProxyID(t *testing.T) {
	buf := captureLog(t)
	const id = "0123456789abcdef0123456789abcdef"
	var seen string
	h := RequestLog()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFrom(r.Context())
		w.WriteHeader(http.StatusTeapot)
	}))
	req := newReq("GET", "/api/v1/session")
	req.Header.Set(RequestIDHeader, id)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if seen != id {
		t.Errorf("context id %q want %q", seen, id)
	}
	if got := rr.Header().Get(RequestIDHeader); got != id {
		t.Errorf("response header %q want %q", got, id)
	}
	lines := requestLines(t, buf)
	if len(lines) != 1 {
		t.Fatalf("want one request line, got %d: %s", len(lines), buf.String())
	}
	if lines[0]["request_id"] != id || lines[0]["status"] != float64(http.StatusTeapot) || lines[0]["path"] != "/api/v1/session" {
		t.Errorf("log line %v", lines[0])
	}
}

// TestRequestLog_MintsForDirectCalls covers the only path where the header is
// absent or unusable: a caller that bypassed nginx.
func TestRequestLog_MintsForDirectCalls(t *testing.T) {
	captureLog(t)
	for name, inbound := range map[string]string{
		"absent":    "",
		"too_long":  strings.Repeat("a", 65),
		"log_forge": "abc\ninjected=1",
		"spaces":    "a b",
	} {
		t.Run(name, func(t *testing.T) {
			h := RequestLog()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			req := newReq("GET", "/login")
			if inbound != "" {
				req.Header.Set(RequestIDHeader, inbound)
			}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			got := rr.Header().Get(RequestIDHeader)
			if got == inbound || !validRequestID(got) || len(got) != 32 {
				t.Errorf("minted id %q (inbound %q)", got, inbound)
			}
		})
	}
}

func TestRequestLog_LogsResolvedClient(t *testing.T) {
	buf := captureLog(t)
	trusted := mustTrust(t, "10.0.0.7/32")
	h := RealIP(trusted)(RequestLog()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))
	req := newReq("GET", "/login")
	req.RemoteAddr = "10.0.0.7:5555"
	req.Header.Set("X-Forwarded-For", "198.51.100.4")
	h.ServeHTTP(httptest.NewRecorder(), req)
	lines := requestLines(t, buf)
	if len(lines) != 1 || lines[0]["client"] != "198.51.100.4" {
		t.Errorf("want the resolved client in the log line, got %v", lines)
	}
}
