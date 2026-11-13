#!/usr/bin/env bash
# coverage-gate.sh, ratcheted coverage gate for certharbor.
#
# Usage:
#   ./scripts/coverage-gate.sh [THRESHOLD]
#
# THRESHOLD defaults to the THRESHOLD environment variable, then to 28
# (Phase 9: measured coverage was 31.1%, more than 5 points above the prior
# 15% floor, so the floor ratchets up with 3 points of headroom rather than
# staying stale). Reaching 80% requires DI seams, a DB interface for
# handlers/ and an issuer interface for le/, tracked as a follow-up refactor
# wave.
#
# Exemptions (kept thin so the number reflects logic, not bootstrap):
#   - None. The root package (certharbor) is NOT excluded and DOES contribute
#     real statements to the profile, tlsreload.go's certReloader and
#     tlsbootstrap.go's TLS-bootstrap helpers (writeInlineRootCert,
#     ensureRootCert, generateSelfSignedCert, ensureUICert, renewUICertOnce)
#     are all covered by root-package _test.go files. A prior version of this
#     comment claimed the root package contributed 0 statements and was
#     therefore invisible to the gate; that was already stale before this
#     fix (tlsreload_test.go predates it) and is corrected here.
#   - Generated / templated glue: none currently exists; if added, exclude via
#     //go:build ignore or a separate module so it never enters coverage.out.
#
# The gate compares the *total* line from `go tool cover -func` which sums all
# non-excluded packages.

set -euo pipefail

THRESHOLD="${1:-${THRESHOLD:-28}}"

PROFILE="${COVERPROFILE:-coverage.out}"

if [ ! -f "$PROFILE" ]; then
    echo "[coverage-gate] running go test to generate $PROFILE …"
    go test ./... -race -covermode=atomic -coverprofile="$PROFILE"
fi

total=$(go tool cover -func="$PROFILE" | awk '/^total:/ { gsub(/%/, "", $3); print $3 }')

if [ -z "$total" ]; then
    echo "[coverage-gate] ERROR: could not parse total coverage from $PROFILE" >&2
    exit 1
fi

echo "[coverage-gate] total coverage: ${total}%  (threshold: ${THRESHOLD}%)"

# awk arithmetic comparison avoids bash floating-point limitations.
if ! awk -v t="$total" -v min="$THRESHOLD" 'BEGIN { exit !(t+0 >= min+0) }'; then
    echo "[coverage-gate] FAIL, coverage ${total}% is below threshold ${THRESHOLD}%" >&2
    exit 1
fi

# The floor must ratchet up as coverage improves: if measured coverage has
# drifted more than 5 points above THRESHOLD, the threshold is stale and
# nobody has raised it to reflect the new measured level.
if awk -v t="$total" -v max="$THRESHOLD" 'BEGIN { exit !(t+0 > max+5) }'; then
    echo "[coverage-gate] FAIL, coverage ${total}% is more than 5 points above threshold ${THRESHOLD}%; raise THRESHOLD in scripts/coverage-gate.sh and .github/workflows/ci.yml to reflect the new measured level" >&2
    exit 1
fi

echo "[coverage-gate] PASS"
