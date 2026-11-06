package main

import (
	"os"
	"regexp"
	"testing"
	"time"

	"step-webui/handlers"
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
