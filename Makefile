# Load .env if it exists (docker compose reads it automatically too).
# Variables defined here are overridden by the actual .env file.
SSH_PORT  ?= 6922
HTTP_PORT ?= 8080
DATA_DIR  ?= ./data

-include .env
export

COMPOSE = docker compose

# ── Stack ──────────────────────────────────────────────────────────────────────

.PHONY: up down build rebuild restart logs

up: ## Start the stack in the background
	$(COMPOSE) up -d

down: ## Stop the stack
	$(COMPOSE) down --remove-orphans

build: ## Build images (incremental)
	$(COMPOSE) build

rebuild: ## Force-rebuild all images from scratch
	$(COMPOSE) build --no-cache

restart: down up ## Restart the stack

logs: ## Tail all service logs
	$(COMPOSE) logs -f

logs-%: ## Tail a single service  (e.g. make logs-tobby)
	$(COMPOSE) logs -f $*

# ── Tests ──────────────────────────────────────────────────────────────────────

.PHONY: test e2e e2e-full

test: ## Run unit tests
	go test . -count=1

e2e: ## Run e2e tests against the already-running stack on SSH_PORT=$(SSH_PORT)
	go test ./tests/ -v -timeout 60s

e2e-full: ## Build, start, run e2e tests, then stop (self-contained)
	go test ./tests/ -v -timeout 300s -run-stack

# ── User / data cleanup ────────────────────────────────────────────────────────

.PHONY: clean-users clean-data clean-volumes clean

clean-users: ## Remove all registered nicks and Logto identity bindings (keeps host key + app volumes)
	@find $(DATA_DIR)/nicks $(DATA_DIR)/identities \
	    -mindepth 1 -delete 2>/dev/null || true
	@echo "Users wiped ($(DATA_DIR)/nicks and $(DATA_DIR)/identities cleared)."

clean-data: ## Wipe the entire local data dir (host key will regenerate on next start)
	rm -rf $(DATA_DIR)
	@echo "$(DATA_DIR) removed."

clean-volumes: ## Remove Docker volumes (tobby SQLite DBs, sshchat data, etc.)
	$(COMPOSE) down -v --remove-orphans

clean: ## Full reset: stop stack, wipe data dir and all Docker volumes
clean: clean-volumes clean-data

# ── Dev helpers ────────────────────────────────────────────────────────────────

.PHONY: ssh

ssh: ## Open an SSH session to the local stack
	ssh -p $(SSH_PORT) -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null localhost

# ── Help ───────────────────────────────────────────────────────────────────────

.PHONY: help
.DEFAULT_GOAL := help

help: ## Show this help
	@echo "Usage: make [target]"
	@echo ""
	@echo "Environment: .env (copy from .env.example). Active values:"
	@echo "  SSH_PORT=$(SSH_PORT)  HTTP_PORT=$(HTTP_PORT)  DATA_DIR=$(DATA_DIR)"
	@echo ""
	@grep -hE '^[a-zA-Z_%/-]+:.*##' $(MAKEFILE_LIST) | \
		awk -F ':.*## ' '{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'
