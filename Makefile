.DEFAULT_GOAL := help

# ──────────────────────────────────────────────────────────────────────────────
# Configuration
# ──────────────────────────────────────────────────────────────────────────────
SECRETS_DIR  := secrets
GO_DIR       := backend
WEB_DIR      := frontend
CLIENT_DIR   := clients/ts
BACKUP_DIR   := backups
COMPOSE      := docker compose

# ──────────────────────────────────────────────────────────────────────────────
# Help, self-documenting via ## comments
# ──────────────────────────────────────────────────────────────────────────────
.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

# ──────────────────────────────────────────────────────────────────────────────
# Bootstrap
# ──────────────────────────────────────────────────────────────────────────────
.PHONY: setup
setup: ## Bootstrap a fresh install: copy .env.example and generate secrets/
	@# Copy .env if absent
	@if [ ! -f .env ]; then \
		cp .env.example .env; \
		echo "  created  .env (from .env.example, edit HOST_IP, WEB_HOSTNAME, UI_HTTPS_PORT, PROVISIONER, TZ, CERTHARBOR_ADMIN_PASSWORD)"; \
	else \
		echo "  skipped  .env already exists"; \
	fi
	@# Create secrets directory
	@mkdir -p $(SECRETS_DIR)
	@chmod 700 $(SECRETS_DIR)
	@# Secret files are 644, not 600: plain (non-Swarm) `docker compose`
	@# bind-mounts file-based secrets preserving the host file's own
	@# owner/mode, it does not remap them to root:root/0444 the way Swarm
	@# secrets do. certharbor-control's container runs as a non-root uid (10001), so a
	@# 600 file owned by the host user is unreadable inside the container;
	@# host-level protection against *other host users* still comes from
	@# $(SECRETS_DIR) itself being 700.
	@# Generate postgres_password
	@if [ ! -f $(SECRETS_DIR)/postgres_password ] || [ "$(FORCE)" = "1" ]; then \
		openssl rand -base64 32 | tr -dc 'A-Za-z0-9' | head -c 32 > $(SECRETS_DIR)/postgres_password; \
		chmod 644 $(SECRETS_DIR)/postgres_password; \
		echo "  created  $(SECRETS_DIR)/postgres_password"; \
	else \
		echo "  skipped  $(SECRETS_DIR)/postgres_password already exists (FORCE=1 to regenerate)"; \
	fi
	@# Generate secret_key
	@if [ ! -f $(SECRETS_DIR)/secret_key ] || [ "$(FORCE)" = "1" ]; then \
		openssl rand -base64 48 | tr -dc 'A-Za-z0-9' | head -c 48 > $(SECRETS_DIR)/secret_key; \
		chmod 644 $(SECRETS_DIR)/secret_key; \
		echo "  created  $(SECRETS_DIR)/secret_key"; \
	else \
		echo "  skipped  $(SECRETS_DIR)/secret_key already exists (FORCE=1 to regenerate)"; \
	fi
	@# Generate ca_password
	@if [ ! -f $(SECRETS_DIR)/ca_password ] || [ "$(FORCE)" = "1" ]; then \
		openssl rand -base64 32 | tr -dc 'A-Za-z0-9' | head -c 32 > $(SECRETS_DIR)/ca_password; \
		chmod 644 $(SECRETS_DIR)/ca_password; \
		echo "  created  $(SECRETS_DIR)/ca_password"; \
	else \
		echo "  skipped  $(SECRETS_DIR)/ca_password already exists (FORCE=1 to regenerate)"; \
	fi
	@echo ""
	@echo "Next steps:"
	@echo "  1. Edit .env, set HOST_IP, WEB_HOSTNAME, UI_HTTPS_PORT, PROVISIONER, TZ, CERTHARBOR_ADMIN_PASSWORD"
	@echo "  2. make up"
	@echo ""
	@echo "To regenerate secrets (existing deployment): make setup FORCE=1"

# ──────────────────────────────────────────────────────────────────────────────
# Docker Compose lifecycle
# ──────────────────────────────────────────────────────────────────────────────
.PHONY: up
up: ## Build images and start all services in detached mode
	$(COMPOSE) up -d --build

.PHONY: down
down: ## Stop and remove containers (volumes are preserved)
	$(COMPOSE) down

.PHONY: restart
restart: down up ## Stop then start all services

.PHONY: logs
logs: ## Stream logs from all services (Ctrl-C to stop)
	$(COMPOSE) logs -f

.PHONY: ps
ps: ## Show container status
	$(COMPOSE) ps

.PHONY: update
update: ## Pull latest images and rebuild
	$(COMPOSE) pull
	$(COMPOSE) up -d --build

# ──────────────────────────────────────────────────────────────────────────────
# Backup
# ──────────────────────────────────────────────────────────────────────────────
.PHONY: backup
backup: ## Dump the database and named volumes into backups/<timestamp>/
	@TS=$$(date +%Y%m%d_%H%M%S); \
	DIR=$(BACKUP_DIR)/$$TS; \
	mkdir -p $$DIR/volumes; \
	echo "  backup dir: $$DIR"; \
	\
	echo "  dumping PostgreSQL …"; \
	$(COMPOSE) exec -T postgres \
		pg_dump -U certharbor certharbor > $$DIR/postgres-certharbor.sql || true; \
	\
	echo "  archiving named volumes …"; \
	for VOL in postgres-data step-ca-data certharbor-control-certs certharbor-control-ssl certharbor-console-ssl certharbor-control-data certharbor-control-uploads; do \
		MOUNT=$$(docker volume inspect certharbor_$$VOL \
			--format '{{.Mountpoint}}' 2>/dev/null || true); \
		if [ -n "$$MOUNT" ] && [ -d "$$MOUNT" ]; then \
			tar -czf $$DIR/volumes/$$VOL.tgz -C $$MOUNT . && \
			echo "    $$VOL.tgz"; \
		fi; \
	done; \
	\
	echo "  writing manifest …"; \
	( \
		printf '{\n'; \
		printf '  "format": "certharbor-makefile-backup-v1",\n'; \
		printf '  "created_at": "%s",\n' "$$(date -Is)"; \
		printf '  "components": [\n'; \
		FIRST=1; \
		for F in $$(find $$DIR -type f ! -name manifest.json | sort); do \
			REL=$${F#$$DIR/}; \
			SIZE=$$(wc -c < $$F | tr -d ' '); \
			SUM=$$(sha256sum $$F | awk '{print $$1}'); \
			if [ "$$FIRST" = "0" ]; then printf ',\n'; fi; \
			FIRST=0; \
			printf '    {"path": "%s", "size": %s, "sha256": "%s"}' "$$REL" "$$SIZE" "$$SUM"; \
		done; \
		printf '\n  ]\n}\n'; \
	) > $$DIR/manifest.json; \
	\
	echo "  done: $$DIR"

# ──────────────────────────────────────────────────────────────────────────────
# Go development
# ──────────────────────────────────────────────────────────────────────────────
.PHONY: test
test: ## Run Go tests with race detector
	cd $(GO_DIR) && go test -race ./...

.PHONY: build
build: ## Build the Go binary
	cd $(GO_DIR) && go build ./...

.PHONY: fmt
fmt: ## Format Go source with gofumpt
	cd $(GO_DIR) && gofumpt -w .

.PHONY: lint
lint: ## Run golangci-lint and check formatting
	cd $(GO_DIR) && golangci-lint run
	cd $(GO_DIR) && gofumpt -l .

.PHONY: cover
cover: ## Run coverage gate
	cd $(GO_DIR) && go test -coverprofile=coverage.out ./... >/dev/null
	cd $(GO_DIR) && COVERPROFILE=coverage.out THRESHOLD=15 bash scripts/coverage-gate.sh

.PHONY: version
version: ## Write the VERSION file into every file that carries a version
	./scripts/sync-version.sh

.PHONY: openapi
openapi: ## Regenerate backend/openapi/openapi.json from the huma-registered operations
	cd $(GO_DIR) && go run ./cmd/openapi -out openapi/openapi.json

.PHONY: hooks
hooks: ## Register the openapi.json merge driver in this clone's git config
	@git config merge.openapi-regen.name "regenerate openapi.json from the merged Go source"
	@git config merge.openapi-regen.driver 'cd backend && go run ./cmd/openapi -out openapi/openapi.json'
	@echo "merge driver registered. Note it fires only on a local git merge or rebase:"
	@echo "GitHub's merge button, squash and merge queue never run local git config (7.2)."

.PHONY: client
client: ## Generate, build and pack the TS client to clients/ts/dist/client.tgz
	cd $(CLIENT_DIR) && npm ci && npm pack --pack-destination dist
	mv $(CLIENT_DIR)/dist/andremmfaria-certharbor-client-*.tgz $(CLIENT_DIR)/dist/client.tgz

.PHONY: frontend-install
frontend-install: ## Install the SPA's deps, then the packed client (run make client first)
	cd $(WEB_DIR) && npm ci && npm install --no-save ../$(CLIENT_DIR)/dist/client.tgz

.PHONY: frontend-build
frontend-build: ## Typecheck and build the SPA
	cd $(WEB_DIR) && npm run build

.PHONY: frontend-lint
frontend-lint: ## ESLint and Prettier on the SPA
	cd $(WEB_DIR) && npm run lint

.PHONY: frontend-typecheck
frontend-typecheck: ## Typecheck the SPA
	cd $(WEB_DIR) && npm run typecheck

.PHONY: frontend-test
frontend-test: ## Run the SPA's Vitest suite
	cd $(WEB_DIR) && npm run test

.PHONY: frontend-sdk-surface
frontend-sdk-surface: ## Fail on spec operations the SPA neither imports nor allowlists
	cd $(WEB_DIR) && npm run check:sdk-surface

# ──────────────────────────────────────────────────────────────────────────────
# End-to-end suite (plans/e2e-tests.md)
# ──────────────────────────────────────────────────────────────────────────────
E2E_DIR     := test/e2e
E2E_PROJECT := $(if $(COMPOSE_PROJECT_NAME),$(COMPOSE_PROJECT_NAME),$(notdir $(CURDIR)))

.PHONY: e2e-install
e2e-install: ## Install the e2e harness deps (plus chromium for a host-side run)
	cd $(E2E_DIR) && npm ci
	cd $(E2E_DIR) && npx playwright install chromium

.PHONY: e2e-fresh
e2e-fresh: ## Destroy every e2e volume and bring the stack back up healthy
	$(COMPOSE) down -v
	$(COMPOSE) up -d --wait

.PHONY: e2e-main
e2e-main: ## PR-tier suite: api then ui, against the long-lived stack
	cd $(E2E_DIR) && npx playwright test --project=api
	cd $(E2E_DIR) && npx playwright test --project=ui

.PHONY: e2e-quick
e2e-quick: ## Pre-push subset (Section 2.8), api only, ~2 minutes
	cd $(E2E_DIR) && npx playwright test --project=api \
		-g 'E2E-AUTH-01|E2E-AUTH-11|E2E-CSRF-01|E2E-RBAC-01|E2E-RBAC-02|E2E-CERT-01|E2E-CERT-09|E2E-HLTH-02|E2E-ADM-01'

.PHONY: e2e-bootstrap
e2e-bootstrap: ## Run one bootstrap scenario: make e2e-bootstrap SCENARIO=fingerprint
	@test -n "$(SCENARIO)" || { echo "usage: make e2e-bootstrap SCENARIO=<selfsigned|provided|ca-down|fingerprint|fatals>"; exit 2; }
	./$(E2E_DIR)/scenario.sh $(SCENARIO)

# Both rate limiters are process-local maps, so a restart clears them, turning
# a multi-minute real-time wait into ~20-30s back to healthy.
.PHONY: e2e-restart-ui
e2e-restart-ui: ## Restart certharbor-control, clearing both process-local rate limiters
	$(COMPOSE) restart certharbor-control
	$(COMPOSE) up -d --wait certharbor-control

# nginx mounts certharbor-console-ssl, so it must go before that volume can, and
# the final up brings it back once the backend has re-issued both leaves.
.PHONY: e2e-reset-ssl
e2e-reset-ssl: ## Remove both leaf volumes (backend and nginx), leaving users and CA state
	$(COMPOSE) rm -sf certharbor-console certharbor-control
	docker volume rm $(E2E_PROJECT)_certharbor-control-ssl $(E2E_PROJECT)_certharbor-console-ssl
	$(COMPOSE) up -d --wait

.PHONY: e2e-seed-history
e2e-seed-history: ## Insert N synthetic cert_history rows: make e2e-seed-history N=25
	@test -n "$(N)" || { echo "usage: make e2e-seed-history N=<count>"; exit 2; }
	$(COMPOSE) exec -T postgres psql -v ON_ERROR_STOP=1 -U certharbor -d certharbor -c \
		"INSERT INTO cert_history (action, cert_name, domain, details, username, role) \
		 SELECT 'issue', 'e2e-seed-' || g, 'seed' || g || '.e2e.invalid', 'synthetic row seeded by make e2e-seed-history', 'e2e-seed', 'admin' \
		 FROM generate_series(1, $(N)) AS g;"

.PHONY: e2e-le-certs
e2e-le-certs: ## Generate the local ACME server's TLS material for the LE leg
	./$(E2E_DIR)/fixtures/pebble/gen-certs.sh

# ──────────────────────────────────────────────────────────────────────────────
# Cleanup
# ──────────────────────────────────────────────────────────────────────────────
.PHONY: clean
clean: ## Remove build artifacts, the generated client, the SPA bundle and old backups (secrets/ and .env are untouched)
	cd $(GO_DIR) && go clean ./...
	rm -rf $(CLIENT_DIR)/src $(CLIENT_DIR)/dist $(WEB_DIR)/dist
	@if [ -d $(BACKUP_DIR) ]; then \
		echo "  removing $(BACKUP_DIR)/"; \
		rm -rf $(BACKUP_DIR); \
	fi

# ──────────────────────────────────────────────────────────────────────────────
# Web origin (nginx)
# ──────────────────────────────────────────────────────────────────────────────
.PHONY: nginx-lint
nginx-lint: ## Fixture nginx against a stub upstream: routing, headers, cache, failure documents (no compose)
	./frontend/nginx-lint/run.sh

.PHONY: proxy-routes-check
proxy-routes-check: ## Assert nginx and the Vite dev proxy both derive from frontend/proxy-prefixes.json
	./scripts/check-proxy-routes.sh

.PHONY: config-keys-check
config-keys-check: ## Assert every web-origin key has a docs/configuration.md row and an .env.example line
	./scripts/check-config-keys.sh

.PHONY: backend-port-check
backend-port-check: ## Assert no compose file, base or override, publishes a host port for certharbor-control
	./scripts/check-backend-port.sh

.PHONY: preview
preview: backend-port-check ## Build both images, run all four containers healthy, smoke-test the origin (before pushing nginx, Vite, CSP or prefix changes)
	$(COMPOSE) up -d --build --wait
	./scripts/check-backend-port.sh --running

# Never --build: that would rebuild the frontend image, Node stage included, to
# start a backend nobody loads the bundled SPA from. make up-backend rebuilds.
# Vite reaches the backend at its container address, since the backend
# publishes no host port, which needs a host that routes to bridge networks
# (Linux, not Docker Desktop). Plain HTTP on localhost, hence SESSION_SECURE
# off and no __Host- prefix in this mode.
.PHONY: dev
dev: ## Start postgres, step-ca and certharbor-control as already built (--wait, no --build), then Vite with hot reload on the host
	SESSION_SECURE=$${SESSION_SECURE:-false} $(COMPOSE) up -d --wait postgres step-ca certharbor-control
	cd $(WEB_DIR) && DEV_BACKEND_URL="https://$$(docker inspect -f '{{with index .NetworkSettings.Networks "step-network"}}{{.IPAddress}}{{end}}' certharbor-control):8443" npm run dev

.PHONY: up-backend
up-backend: ## Rebuild and start postgres, step-ca and certharbor-control only
	$(COMPOSE) up -d --build --wait postgres step-ca certharbor-control

.PHONY: up-web
up-web: ## Rebuild and restart certharbor-console alone against the running backend
	$(COMPOSE) up -d --build --wait --no-deps certharbor-console

.PHONY: logs-ui
logs-ui: ## Stream certharbor-control's logs (Ctrl-C to stop)
	$(COMPOSE) logs -f certharbor-control

.PHONY: logs-web
logs-web: ## Stream certharbor-console's logs (Ctrl-C to stop)
	$(COMPOSE) logs -f certharbor-console

.PHONY: nginx-test
nginx-test: ## Run nginx -t inside the running certharbor-console
	$(COMPOSE) exec certharbor-console nginx -t

.PHONY: web-reload
web-reload: ## Validate and reload nginx in the running certharbor-console, no restart
	$(COMPOSE) exec certharbor-console sh -c 'nginx -t && nginx -s reload'
