package handlers

import (
	"testing"
	"time"
)

// TestCertExpiryStatus pins the three boundaries Section 6's note on the
// certificate DTOs asks for, at the ExpiringSoonDays threshold exactly:
// already past NotAfter is expired, inside the window (inclusive) is
// warning, past it is ok. These are also badgeClass's thresholds now that
// it calls this function, so a regression here would change the
// server-rendered certificate pages too.
func TestCertExpiryStatus(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		t    *time.Time
		want string
	}{
		{"nil expiry", nil, "ok"},
		{"already expired", timePtr(now.Add(-24 * time.Hour)), "expired"},
		{"expires now", timePtr(now), "expired"},
		{"one day left", timePtr(now.Add(36 * time.Hour)), "warning"},
		{"exactly at the threshold", timePtr(now.Add(ExpiringSoonDays * 24 * time.Hour)), "warning"},
		{"just past the threshold", timePtr(now.Add((ExpiringSoonDays + 2) * 24 * time.Hour)), "ok"},
		{"far in the future", timePtr(now.Add(365 * 24 * time.Hour)), "ok"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CertExpiryStatus(c.t); got != c.want {
				t.Errorf("CertExpiryStatus(%s) = %q, want %q", c.name, got, c.want)
			}
		})
	}
}

func timePtr(t time.Time) *time.Time { return &t }
