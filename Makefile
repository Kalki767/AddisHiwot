# AddisHiwot developer commands. `make help` lists them.
# Targets marked "placeholder" print the plan day that will fill them in.

SHELL := /usr/bin/env bash
COMPOSE := docker compose

.DEFAULT_GOAL := help
.PHONY: help test lint up down down-v ps logs psql mobile-test e2e ai-smoke ai-eval

help: ## List the available commands
	@grep -E '^[a-z0-9-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'

test: ## Run every test (placeholder, Day 5)
	@echo "make test: no tests yet, the Go tests arrive on Day 5"

lint: ## Run the linters (Go linters arrive on Day 5)
	$(COMPOSE) --env-file .env.example config --quiet
	@echo "docker-compose.yml: valid"

# Created once from the example; never overwritten, so local edits survive.
.env:
	cp .env.example .env
	@echo "Created .env from .env.example. Change the passwords in it if you like."

up: .env ## Start the local Docker stack and wait until every container is healthy
	$(COMPOSE) up --detach --wait

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
