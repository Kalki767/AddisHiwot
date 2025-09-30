# AddisHiwot developer commands. `make help` lists them.
# Targets marked "placeholder" print the plan day that will fill them in.

SHELL := /usr/bin/env bash
COMPOSE := docker compose
# golangci-lint runs from its pinned image; the two volumes cache Go modules,
# the Go toolchain and lint results between runs.
GOLANGCI := docker run --rm -v $(CURDIR)/backend:/app -w /app \
	-v ah-golangci-cache:/root/.cache -v ah-go-mod:/go/pkg/mod \
	golangci/golangci-lint:v2.15.0@sha256:fbfcfd12a6f8d7f1b97b3362ca80fc2a244d46863148c73cb88e7793af96c481

.DEFAULT_GOAL := help
.PHONY: help test lint env-check up down down-v ps logs psql mobile-test e2e ai-smoke ai-eval

help: ## List the available commands
	@grep -E '^[a-z0-9-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'

test: ## Run every test (Go, with the race detector)
	cd backend && go test -race -count=1 ./...

lint: ## Run go vet, golangci-lint and check docker-compose.yml
	cd backend && go vet ./...
	$(GOLANGCI) golangci-lint run ./...
	$(COMPOSE) --env-file .env.example config --quiet
	@echo "docker-compose.yml: valid"

# Created once from the example; never overwritten, so local edits survive.
.env:
	cp .env.example .env
	@echo "Created .env from .env.example. Change the passwords in it if you like."

# .env is never overwritten, so settings added to .env.example later must be copied by hand.
env-check: .env ## Fail if .env lacks a setting that .env.example has
	@missing=$$(comm -23 <(grep -oE '^[A-Z0-9_]+=' .env.example | sort -u) <(grep -oE '^[A-Z0-9_]+=' .env | sort -u) | tr -d '='); \
	if [ -n "$$missing" ]; then \
		echo "Your .env is missing settings that .env.example has:"; \
		echo "$$missing" | sed 's/^/  /'; \
		echo "Copy them from .env.example into .env, then run the command again."; \
		exit 1; \
	fi

up: env-check ## Build the API, start the local Docker stack and wait until every container is healthy
	$(COMPOSE) up --detach --build --wait

down: ## Stop the local Docker stack (data is kept)
	$(COMPOSE) down

down-v: ## Stop the local Docker stack and delete its data volumes
	$(COMPOSE) down --volumes

ps: ## Show the containers and their health
	$(COMPOSE) ps

logs: ## Follow the containers' logs
	$(COMPOSE) logs --follow --tail=100

psql: ## Open a psql shell on the dev database
	$(COMPOSE) exec postgres sh -c 'psql -U "$$POSTGRES_USER" -d "$$POSTGRES_DB"'

mobile-test: ## Run the Flutter tests (placeholder, Day 13)
	@echo "make mobile-test: the Flutter app arrives on Day 13"

e2e: ## Run the end-to-end tests (placeholder, Day 64)
	@echo "make e2e: end-to-end tests arrive on Day 64"

ai-smoke: ## Make one real AI call (placeholder, Day 107)
	@echo "make ai-smoke: arrives on Day 107"

ai-eval: ## Run the AddisBot eval set (placeholder, Day 119)
	@echo "make ai-eval: arrives on Day 119"
