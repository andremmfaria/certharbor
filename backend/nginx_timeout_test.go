package main

import (
	"os"
	"regexp"
	"testing"
	"time"

	"certharbor/handlers"
)

// TestNginxReadTimeoutExceedsBackendOperations: nginx gives up on a proxied
// request after proxy_read_timeout and answers 504, which reads as a broken
// backend. Every long backend operation must therefore finish first. A new
// operation with a longer ceiling belongs in this list, and then fails here
// rather than in production.
func TestNginxReadTimeoutExceedsBackendOperations(t *testing.T) {
	raw, err := os.ReadFile("../frontend/nginx.conf.template")
	if err != nil {
		t.Fatalf("reading the nginx template: %v", err)
	}
	m := regexp.MustCompile(`(?m)^\s*proxy_read_timeout\s+(\d+)s;`).FindAllSubmatch(raw, -1)
	if len(m) != 1 {
		t.Fatalf("want exactly one proxy_read_timeout in seconds, found %d", len(m))
	}
	readTimeout, err := time.ParseDuration(string(m[0][1]) + "s")
	if err != nil {
		t.Fatal(err)
	}
	operations := map[string]time.Duration{
		"handlers.PGDumpTimeout": handlers.PGDumpTimeout,
	}
	for name, d := range operations {
		if readTimeout <= d {
			t.Errorf("proxy_read_timeout %v does not exceed %s (%v)", readTimeout, name, d)
		}
	}
}

// TestGoWriteTimeoutExceedsBackendOperations is Phase 6's regression test
// for the backup timeout defect: nginx's proxy_read_timeout exceeding
// PGDumpTimeout (asserted above) is not enough on its own, because the Go
// http.Server's own WriteTimeout cuts a long-running response off first if
// it is shorter. A backup that takes more than WriteTimeout is severed by
// Go regardless of what nginx would have tolerated. This test compares
// serverWriteTimeout (main.go) directly against handlers.PGDumpTimeout,
// which is exactly the comparison nginx_timeout_test.go's existing test
// never made, and which let the original 60s-vs-2min mismatch ship.
func TestGoWriteTimeoutExceedsBackendOperations(t *testing.T) {
	operations := map[string]time.Duration{
		"handlers.PGDumpTimeout": handlers.PGDumpTimeout,
	}
	for name, d := range operations {
		if serverWriteTimeout <= d {
			t.Errorf("serverWriteTimeout %v does not exceed %s (%v)", serverWriteTimeout, name, d)
		}
	}
}
