# AddisHiwot developer commands. `make help` lists them.
# Targets marked "placeholder" print the plan day that will fill them in.

SHELL := /usr/bin/env bash
SCRIPTS := .githooks/commit-msg $(wildcard scripts/*.sh)

.DEFAULT_GOAL := help
.PHONY: help hooks test lint up down logs psql mobile-test e2e ai-smoke ai-eval

help: ## List the available commands
	@grep -E '^[a-z0-9-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'

hooks: ## Use the repo's git hooks (commit-message checker)
	git config core.hooksPath .githooks
	@echo "commit-msg hook active"

test: ## Run every test
	scripts/test-commit-msg.sh

lint: ## Check scripts for syntax errors
	@for f in $(SCRIPTS); do bash -n "$$f" || exit 1; echo "ok  $$f"; done

up: ## Start the local Docker stack (placeholder, Day 4)
	@echo "make up: the Docker stack arrives on Day 4"

down: ## Stop the local Docker stack (placeholder, Day 4)
	@echo "make down: the Docker stack arrives on Day 4"

logs: ## Follow container logs (placeholder, Day 4)
	@echo "make logs: the Docker stack arrives on Day 4"

psql: ## Open a psql shell on the dev database (placeholder, Day 4)
	@echo "make psql: the Docker stack arrives on Day 4"

mobile-test: ## Run the Flutter tests (placeholder, Day 13)
	@echo "make mobile-test: the Flutter app arrives on Day 13"

e2e: ## Run the end-to-end tests (placeholder, Day 64)
	@echo "make e2e: end-to-end tests arrive on Day 64"

ai-smoke: ## Make one real AI call (placeholder, Day 107)
	@echo "make ai-smoke: arrives on Day 107"

ai-eval: ## Run the AddisBot eval set (placeholder, Day 119)
	@echo "make ai-eval: arrives on Day 119"
