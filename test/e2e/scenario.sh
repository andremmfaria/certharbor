#!/usr/bin/env bash
# One entry point per bootstrap scenario (Section 2.7.2). Each names its compose
# overrides and its grep filter, then hands off to the infra project, which owns
# the compose lifecycle for its own disposable stack.
#
# The scenario runs against a copy of .env, never the checkout's own, so a case
# that flips UI_TLS_MODE or plants a wrong CA_FINGERPRINT leaves nothing behind.
set -euo pipefail

scenario="${1:-}"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
e2e_dir="$repo_root/test/e2e"

usage() {
  echo "usage: $0 <selfsigned|provided|ca-down|fingerprint|fatals>" >&2
  exit 2
}
[[ -n "$scenario" ]] || usage

# In CI the image job already built both application images, so the driver
# composes the image override in and the specs never pass --build.
image_override=""
if [[ "${E2E_USE_PREBUILT_IMAGE:-}" == "1" ]]; then
  image_override="compose.e2e-image.yml"
fi

case "$scenario" in
  selfsigned)  overrides="";                                            filter='E2E-BOOT-04' ;;
  provided)    overrides="";                                            filter='E2E-BOOT-03' ;;
  ca-down)     overrides="compose.e2e-nodeps.yml";                      filter='E2E-BOOT-02|E2E-BOOT-09' ;;
  fingerprint) overrides="compose.e2e-fingerprint.yml";                 filter='E2E-BOOT-01|E2E-BOOT-05|E2E-BOOT-06' ;;
  fatals)      overrides="compose.e2e-fatals.yml:compose.e2e-nodeps.yml"; filter='E2E-BOOT-07' ;;
  *)           usage ;;
esac

# E2E-BOOT-09 needs the fingerprint override's writable, empty ROOT_CERT path;
# E2E-BOOT-02 needs the stock read-only mount so a root cert exists. They share a
# scenario, so the spec composes the extra file itself per case.
if [[ "$scenario" == "ca-down" ]]; then
  export E2E_CA_DOWN_FINGERPRINT_OVERRIDE="compose.e2e-fingerprint.yml"
fi

if [[ -n "$image_override" ]]; then
  overrides="${overrides:+$overrides:}$image_override"
fi

# The bootstrap tests run below the HTTP layer and dial the backend directly,
# so no scenario brings nginx up.
overrides="${overrides:+$overrides:}compose.e2e-noweb.yml"

env_file="$repo_root/.env.e2e-$scenario"
cp "$repo_root/.env" "$env_file"
trap 'rm -f "$env_file"' EXIT

# Case (b) of the fatals scenario needs the admin password absent, so that
# scenario is the one place the driver does not set one.
if [[ "$scenario" != "fatals" ]]; then
  if ! grep -qE '^CERTHARBOR_ADMIN_PASSWORD=.+' "$env_file"; then
    admin_pw="$(openssl rand -base64 24)"
    echo "::add-mask::${admin_pw}"
    sed -i '/^#\?[[:space:]]*CERTHARBOR_ADMIN_PASSWORD=/d' "$env_file"
    echo "CERTHARBOR_ADMIN_PASSWORD=${admin_pw}" >> "$env_file"
    export CERTHARBOR_ADMIN_PASSWORD="$admin_pw"
  else
    CERTHARBOR_ADMIN_PASSWORD="$(sed -n 's/^CERTHARBOR_ADMIN_PASSWORD=//p' "$env_file" | head -1)"
    export CERTHARBOR_ADMIN_PASSWORD
  fi
fi

# Both containers read the mounted secrets as their own non-root uid, which is
# never the checkout's owner. Mode 600 denies them (step-ca is uid 1000,
# certharbor 10001) and the CA never finishes booting.
chmod 644 "$repo_root"/secrets/* 2>/dev/null || true

# compose.e2e-noweb.yml hides certharbor-web from every compose call the specs
# make, so an origin left running by an earlier main-suite stack in the same
# project would survive their `down -v`, holding step-network and its volume.
docker compose -f "$repo_root/docker-compose.yml" --env-file "$env_file" rm -sf certharbor-web >/dev/null

export E2E_COMPOSE_OVERRIDES="$overrides"
export E2E_ENV_FILE="$env_file"
export E2E_SCENARIO="$scenario"

echo "scenario=$scenario overrides=[${overrides:-none}] filter=$filter env_file=$env_file"

# Not exec: the EXIT trap must still run, or the scenario's .env copy, which
# carries the admin password, outlives the run.
cd "$e2e_dir"
npx playwright test --project=infra -g "$filter"
