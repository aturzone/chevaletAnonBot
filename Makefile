# Makefile for Chevalet Anonymous Telegram Bot
# Usage: make <target>
# Example: make up

.PHONY: help gooz up down restart logs logs-bot logs-db build rebuild status shell db-shell backup restore clean dev-up dev-down test ensure-dynset go-build go-test go-test-race go-vet go-fmt go-run go-tidy

# Default target - shows help
.DEFAULT_GOAL := help

##@ General

help: ## Show this help message
	@echo "Chevalet Anonymous Telegram Bot - Available Commands:"
	@echo ""
	@echo "When updated the project, do \"make deploy\""
	@echo ""
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Go Development

go-build: ## Compile all packages
	go build ./...

go-test: ## Run the test suite
	go test ./...

go-test-race: ## Run the test suite with the race detector (needs a C compiler)
	go test -race -count=1 ./...

go-vet: ## Run go vet
	go vet ./...

go-fmt: ## Format all Go files in place (gofmt -w)
	gofmt -w .

go-run: ## Run the bot locally (needs a populated .env + reachable PostgreSQL)
	go run ./cmd/bot

go-tidy: ## Tidy go.mod / go.sum
	go mod tidy

##@ Docker Operations

ensure-dynset: ## Create dynamic_settings.json if missing (needed for the bind mount)
	@test -f dynamic_settings.json || (echo '{}' > dynamic_settings.json && echo "created empty dynamic_settings.json")

up: ensure-dynset ## Start all services (production)
	@echo "Starting services..."
	docker-compose up -d
	@echo "Services started! Check logs with: make logs"

down: ## Stop all services
	@echo "Stopping services..."
	docker-compose down
	@echo "Services stopped."

restart: ## Restart all services
	@echo "Restarting services..."
	docker-compose restart
	@echo "Services restarted."

build: ## Build Docker images
	@echo "Building images..."
	docker-compose build
	@echo "Build complete."

rebuild: ensure-dynset ## Rebuild and restart services (use after code changes)
	@echo "Rebuilding and restarting..."
	docker-compose down
	docker-compose up -d --build
	@echo "Rebuild complete! Check logs with: make logs"

##@ Logs & Monitoring

logs: ## Show logs from all services (follow mode)
	docker-compose logs -f

logs-bot: ## Show logs from bot only (follow mode)
	docker-compose logs -f bot

logs-db: ## Show logs from database only (follow mode)
	docker-compose logs -f postgres

logs-tail: ## Show last 100 lines from bot
	docker-compose logs --tail=100 bot

status: ## Show status of all containers
	docker-compose ps

##@ Development

shell: ## Open bash shell in bot container
	docker-compose exec bot bash

db-shell: ## Open PostgreSQL shell
	docker-compose exec postgres psql -U root -d mydatabase

db-drop-tables: ## Drop all tables from database (WARNING: Deletes all data but keeps DB!)
	@echo "WARNING: This will drop all tables!"
	@read -p "Are you sure? Type 'yes' to continue: " confirm; \
	if [ "$$confirm" = "yes" ]; then \
		docker-compose exec postgres psql -U root -d mydatabase -c "DROP TABLE IF EXISTS users, blocks, cids, reports CASCADE;"; \
		echo "All tables dropped. Restart bot to recreate: make restart"; \
	else \
		echo "Cancelled."; \
	fi

##@ Database Operations

backup: ## Backup database to ./backups/ (simple, 30-day retention)
	@echo "Creating backup..."
	@bash backup.sh
	@echo "Backup complete!"

backup-tiered: ## Backup database with tiered retention (hourly/daily/weekly)
	@echo "Creating backup with tiered retention..."
	@bash backup-tiered.sh
	@echo "Backup complete!"

backup-setup-auto: ## Setup automated hourly backups (Linux/WSL)
	@echo "Setting up automated hourly backups..."
	@bash setup-backup-cron.sh

restore: ## Restore database from backup (Usage: make restore FILE=backups/backup_file.sql.gz)
	@if [ -z "$(FILE)" ]; then \
		echo "Error: Please specify backup file"; \
		echo "Usage: make restore FILE=backups/backup_mydatabase_20250128_120000.sql.gz"; \
		echo ""; \
		echo "Available backups:"; \
		ls -lh ./backups/ 2>/dev/null || echo "No backups found"; \
		exit 1; \
	fi
	@echo "Restoring from $(FILE)..."
	@bash restore.sh $(FILE)

db-reset: ## Reset database (WARNING: Deletes all data!)
	@echo "WARNING: This will delete all data!"
	@read -p "Are you sure? Type 'yes' to continue: " confirm; \
	if [ "$$confirm" = "yes" ]; then \
		docker-compose down -v; \
		docker-compose up -d postgres; \
		sleep 5; \
		docker-compose up -d bot; \
		echo "Database reset complete."; \
	else \
		echo "Cancelled."; \
	fi

##@ Maintenance

clean: ## Remove stopped containers and unused images
	@echo "Cleaning up Docker resources..."
	docker-compose down --remove-orphans
	docker system prune -f
	@echo "Cleanup complete."

clean-all: ## Remove everything including volumes (WARNING: Deletes database!)
	@echo "WARNING: This will delete the database!"
	@read -p "Are you sure? Type 'yes' to continue: " confirm; \
	if [ "$$confirm" = "yes" ]; then \
		docker-compose down -v --remove-orphans; \
		docker system prune -af; \
		echo "All Docker resources removed."; \
	else \
		echo "Cancelled."; \
	fi

logs-clean: ## Remove old log files (older than 30 days)
	@echo "Removing old log files..."
	find ./logs -name "*.log" -mtime +30 -delete 2>/dev/null || true
	find ./backups -name "*.sql.gz" -mtime +60 -delete 2>/dev/null || true
	@echo "Old logs cleaned."

update: ## Pull latest code and rebuild
	@echo "Updating application..."
	docker-compose down
	git pull
	docker-compose up -d --build
	@echo "Update complete!"

##@ Testing & Debugging

check-health: ## Check health status of all containers
	@docker-compose ps --format "table {{.Name}}\t{{.Status}}\t{{.Health}}"

##@ Quick Actions

quick-restart: down up logs-tail ## Quick restart: down, up, show logs

deploy: rebuild logs-tail ## Deploy: rebuild and show logs

emergency-stop: ## Emergency stop (force stop all containers)
	@echo "Emergency stop initiated..."
	docker-compose kill
	docker-compose down
	@echo "All services forcefully stopped."
