ifneq (,$(wildcard .env))
include .env
export
endif
COMPOSE=docker compose -f deploy/docker-compose.yml
.PHONY: help up down down-v logs build test test-integration cover lint fmt vet migrate demo observability-smoke worker-recovery release-verify
help: ## показать команды
	@awk 'BEGIN {FS=":.*## "} /^[a-zA-Z0-9_-]+:.*## / {printf "%-20s %s\n",$$1,$$2}' $(MAKEFILE_LIST)
up: ## поднять полный стек
	$(COMPOSE) up -d --build --wait
down: ## остановить стек
	$(COMPOSE) down
down-v: ## остановить и удалить локальные данные
	$(COMPOSE) down -v
logs: ## смотреть логи
	$(COMPOSE) logs -f
build: ## собрать
	go build ./...
test: ## unit и race
	go test -race ./...
test-integration: ## PostgreSQL integration
	go test -race -tags=integration ./...
cover: ## покрытие
	./scripts/check-coverage.sh
fmt: ## форматирование
	gofmt -w .
vet: ## vet
	go vet ./...
lint: ## golangci-lint
	golangci-lint run ./...
migrate: ## применить миграции отдельной командой
	$(COMPOSE) run --rm --entrypoint /usr/local/bin/migrate hookline-api
demo: ## минимальное end-to-end демо
	./scripts/demo.sh
observability-smoke: ## проверить Prometheus, workers и Grafana
	./scripts/observability-smoke.sh
worker-recovery: ## проверить восстановление после SIGKILL воркеров
	./scripts/worker-recovery.sh
release-verify: ## выполнить автоматическую проверку релиза v1
	./scripts/release-verify.sh
