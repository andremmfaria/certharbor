//go:build integration

package db

import (
	"context"
	"testing"
	"time"
)

// assertTiedTimestampPagination is the reusable form of 5.6's exit
// criterion (plans/frontend-backend-split.md Section 2 and Phase 5): a list
// of 60 rows sharing one created_at, paged at 10, must return 60 distinct
// ids across 6 pages with no repeat and no omission. db/history.go and
// db/authlog.go both got the identical total-order fix (ORDER BY
// created_at DESC, id DESC) in this change, so both call this with their
// own page function rather than each carrying a copy of the assertion body.
// Phase 5 step 2's security/events and audit-events specs, and Phase 7's
// acme/events, are expected to call this same helper against their own
// page function instead of re-deriving the property.
func assertTiedTimestampPagination(t *testing.T, total int, pageSize int, page func(page, pageSize int) ([]int, int, error)) {
	t.Helper()

	seen := map[int]bool{}
	wantPages := (total + pageSize - 1) / pageSize
	for p := 1; p <= wantPages; p++ {
		ids, reportedTotal, err := page(p, pageSize)
		if err != nil {
			t.Fatalf("page %d: %v", p, err)
		}
		if reportedTotal != total {
			t.Fatalf("page %d: total = %d, want %d", p, reportedTotal, total)
		}
		if len(ids) != pageSize {
			t.Fatalf("page %d: got %d ids, want %d", p, len(ids), pageSize)
		}
		for _, id := range ids {
			if seen[id] {
				t.Fatalf("page %d: id %d repeats a row already seen on an earlier page", p, id)
			}
			seen[id] = true
		}
	}
	if len(seen) != total {
		t.Fatalf("saw %d distinct ids across %d pages, want %d (a row was omitted)", len(seen), wantPages, total)
	}
}

// TestGetHistory_TiedTimestampPaginationHasNoRepeatOrOmission is the
// history half of 5.6's exit criterion: 60 cert_history rows sharing one
// created_at, paged at 10, must produce 60 distinct ids across 6 pages.
// Before the ORDER BY created_at DESC, id DESC fix, Postgres is free to
// return ties in any order per query, so the same LIMIT/OFFSET window can
// repeat or skip rows across two calls with no other change.
func TestGetHistory_TiedTimestampPaginationHasNoRepeatOrOmission(t *testing.T) {
	d := openTestDB(t)
	tied := time.Now().UTC().Truncate(time.Second)
	const total = 60
	for i := 0; i < total; i++ {
		if _, err := d.Exec( //nolint:noctx // test setup
			`INSERT INTO cert_history (action,cert_name,domain,details,username,role,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			"issue", "tied-cert", "tied.example.com", "", "tester", "admin", tied,
		); err != nil {
			t.Fatalf("insert row %d: %v", i, err)
		}
	}

	assertTiedTimestampPagination(t, total, 10, func(page, pageSize int) ([]int, int, error) {
		rows, gotTotal, err := GetHistory(d, nil, "", page, pageSize)
		if err != nil {
			return nil, 0, err
		}
		ids := make([]int, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
		return ids, gotTotal, nil
	})
}

// TestGetAuthLogs_TiedTimestampPaginationHasNoRepeatOrOmission is the
// auth_log half of the same exit criterion (security/events reuses
// GetAuthLogs starting in Phase 5 step 2; this is the db-layer proof the
// total-order fix also covers it, ahead of that operation existing).
func TestGetAuthLogs_TiedTimestampPaginationHasNoRepeatOrOmission(t *testing.T) {
	d := openTestDB(t)
	tied := time.Now().UTC().Truncate(time.Second)
	const total = 60
	for i := 0; i < total; i++ {
		if _, err := d.Exec( //nolint:noctx // test setup
			`INSERT INTO auth_log (username,ip,success,reason,created_at) VALUES ($1,$2,$3,$4,$5)`,
			"tied-user", "127.0.0.1", true, "", tied,
		); err != nil {
			t.Fatalf("insert row %d: %v", i, err)
		}
	}

	assertTiedTimestampPagination(t, total, 10, func(page, pageSize int) ([]int, int, error) {
		rows, gotTotal, err := GetAuthLogs(d, "", "", page, pageSize)
		if err != nil {
			return nil, 0, err
		}
		ids := make([]int, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, r.ID)
		}
		return ids, gotTotal, nil
	})
}

// TestListACMELogsPaged_TiedTimestampPaginationHasNoRepeatOrOmission is
// le_logs' half of the same exit criterion (Phase 7: GET
// /api/v1/acme/events joins history, security/events and audit-events on
// this shared convention): 60 rows sharing one created_at, paged at 10,
// must produce 60 distinct ids across 6 pages with no repeat or omission.
func TestListACMELogsPaged_TiedTimestampPaginationHasNoRepeatOrOmission(t *testing.T) {
	d := openTestDB(t)
	if err := InitACMESchema(d); err != nil {
		t.Fatalf("InitACMESchema: %v", err)
	}
	tied := time.Now().UTC().Truncate(time.Second)
	const total = 60
	for i := 0; i < total; i++ {
		if _, err := d.Exec( //nolint:noctx // test setup
			`INSERT INTO le_logs (domain,action,message,created_at) VALUES ($1,$2,$3,$4)`,
			"tied.example.com", "issue", "", tied,
		); err != nil {
			t.Fatalf("insert row %d: %v", i, err)
		}
	}

	assertTiedTimestampPagination(t, total, 10, func(page, pageSize int) ([]int, int, error) {
		logs, gotTotal, err := ListACMELogsPaged(context.Background(), d, "", page, pageSize)
		if err != nil {
			return nil, 0, err
		}
		ids := make([]int, 0, len(logs))
		for _, l := range logs {
			ids = append(ids, l.ID)
		}
		return ids, gotTotal, nil
	})
}
