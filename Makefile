# Jarvis Office developer commands. Run from the repo root.
SHELL := /bin/bash
COMPOSE := docker compose -f deploy/docker-compose.yml
DB_URL ?= postgres://jarvis:jarvis@localhost:5432/jarvis?sslmode=disable
TEST_DB_URL ?= postgres://jarvis:jarvis@localhost:5432/jarvis_test?sslmode=disable
# make e2e RUN='TestJourney/04' runs one step (go test -run pattern).
RUN ?= .

.PHONY: help up up-all down db-reset migrate seed run run-fakes build test test-go test-go-unit test-flutter e2e smoke lint fmt app-run enroll

help: ## List targets
	@grep -E '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-14s %s\n", $$1, $$2}'

up: ## Start Postgres (waits until healthy)
	$(COMPOSE) up -d --wait postgres

up-all: ## Start Postgres + api container
	$(COMPOSE) --profile api up -d --build --wait

down: ## Stop containers
	$(COMPOSE) --profile api down

db-reset: ## Drop the dev database volume and start fresh
	$(COMPOSE) --profile api down -v
	$(MAKE) up

migrate: up ## Apply SQL migrations to the dev DB
	cd backend && DATABASE_URL="$(DB_URL)" go run ./cmd/migrate

seed: migrate ## Load backend/seed/* into the dev DB (idempotent)
	cd backend && DATABASE_URL="$(DB_URL)" go run ./cmd/migrate -seed

run: up ## Run the api on the host (reads backend/.env)
	cd backend && DATABASE_URL="$(DB_URL)" go run ./cmd/api

run-fakes: up ## Run the api with fake Reap + LLM (offline)
	cd backend && DATABASE_URL="$(DB_URL)" FAKES=true go run ./cmd/api

build: ## Build backend + Flutter web
	cd backend && go build ./...
	cd app && flutter build web

test: test-go test-flutter ## All offline tests (Go unit + integration + Flutter)

test-go-unit: ## Go unit tests only (no DB)
	cd backend && go test ./...

test-go: up ## Go unit + Postgres integration tests (TEST_DATABASE_URL -> jarvis_test DB)
	cd backend && TEST_DATABASE_URL="$(TEST_DB_URL)" go test -p 1 ./...

test-flutter: ## Flutter analyze + widget tests
	cd app && flutter analyze && flutter test

e2e: up ## End-to-end: real api binary + fake Reap/OpenAI servers + Postgres (DB jarvis_e2e, build tag e2e)
	cd backend && TEST_DATABASE_URL="$(TEST_DB_URL)" go test -p 1 -tags e2e -count=1 -timeout 10m -run '$(RUN)' -v ./e2e/...

smoke: ## Real-API smoke tests (Reap sandbox + OpenAI). Needs backend/.env. Costs tokens.
	cd backend && go test -tags smoke -count=1 -run Smoke ./...

lint: ## go vet + flutter analyze
	cd backend && go vet ./...
	cd app && flutter analyze

fmt: ## Format Go + Dart
	cd backend && gofmt -w .
	cd app && dart format lib test

enroll: ## Enroll the office card via the Reap-hosted page (api must be running)
	./scripts/enroll-card.sh

app-run: ## Run the Flutter web app on :5173 against the local api
	cd app && flutter run -d chrome --web-port 5173 --dart-define=API_BASE_URL=http://localhost:8080

demo: ## Start everything for a demo (Docker api, web, tunnel) and print the URL
	scripts/demo.sh start

demo-status: ## Show demo stack status
	scripts/demo.sh status

demo-stop: ## Stop the demo stack
	scripts/demo.sh stop

demo-redeploy: ## Rebuild api + web after code changes, keeping the tunnel URL
	scripts/demo.sh redeploy
