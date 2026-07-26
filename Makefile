# ============================================================================
# Команды разработки.
#
# make help   — список всех команд (надо сделать issue #28)
# ============================================================================


# --- настройки -------------------------------------------------------------

# Версия линтера и goose
GOLANGCI_VERSION ?= v2.12.2
GOOSE_VERSION ?= v3.27.1

# Подключаем .env, чтобы переменные были доступны и в make, и в дочерних
# командах (например, `make run` увидит DATABASE_URL).
ifneq (,$(wildcard .env))
    include .env
    export
endif

# Локальные инструменты в ./bin внутри проекта
LOCAL_BIN := bin
GOLANGCI  := $(LOCAL_BIN)/golangci-lint
COMPOSE := docker compose -f deploy/docker-compose.yml
GOOSE_BIN := $(LOCAL_BIN)/goose
GOOSE := GOOSE_DRIVER=postgres GOOSE_DBSTRING="$(DATABASE_URL)" $(GOOSE_BIN) -dir migrations

# Если написать просто `make` — покажем справку, а не запустим первую цель.
.DEFAULT_GOAL := help
# .PHONY перечислит цели, которые НЕ являются именами файлов.
.PHONY: help up down down-v logs ps psql run test test-short fmt vet tidy \
        lint lint-fix tools migrate migrate-down migrate-status clean check


# --- справка ---------------------------------------------------------------

help:


# --- инфраструктура (docker) -----------------------------------------------

up: ## поднять базу данных и дождаться её готовности
	$(COMPOSE) up -d --wait
	@echo ">> база готова: localhost:$(POSTGRES_PORT)"

down: ## погасить контейнеры (данные базы сохранятся)
	$(COMPOSE) down

down-v: ## погасить контейнеры И УДАЛИТЬ данные базы (полный сброс)
	$(COMPOSE) down -v

logs: ## смотреть логи контейнеров (Ctrl+C — выйти)
	$(COMPOSE) logs -f

ps: ## показать, какие контейнеры запущены
	$(COMPOSE) ps

psql: ## открыть консоль psql внутри контейнера с базой
	$(COMPOSE) exec postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB)


# --- разработка ------------------------------------------------------------

run: ## запустить приложение локально
	go run ./cmd/hookline

test: ## прогнать все тесты с детектором гонок
	go test -race ./...

test-short: ## быстрые тесты без детектора гонок (для частых прогонов)
	go test ./...

fmt: ## отформатировать весь код
	go fmt ./...

vet: ## встроенный анализатор частых ошибок
	go vet ./...

tidy: ## привести go.mod и go.sum в порядок
	go mod tidy

check: fmt vet lint test ## всё сразу: формат, анализ, линтер, тесты

# --- линтер ----------------------------------------------------------------

# Эта цель — файл. Make выполнит её ТОЛЬКО если файла ./bin/golangci-lint нет.
$(GOLANGCI):
	@echo ">> golangci-lint не найден, скачиваю $(GOLANGCI_VERSION) в ./bin ..."
	@mkdir -p $(LOCAL_BIN)
	@curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b ./$(LOCAL_BIN) $(GOLANGCI_VERSION)
	@echo ">> готово: $(GOLANGCI)"

tools: $(GOLANGCI) ## поставить локальные инструменты в ./bin
	@./$(GOLANGCI) --version
 
lint: $(GOLANGCI) ## проверить код
	./$(GOLANGCI) run ./...
 
lint-fix: $(GOLANGCI) ##линтер + автоисправление того, что можно починить само
	$(GOLANGCI) run --fix ./...

 
# --- goose -----------------------------------------------------------------

# make поставит goose, только если файла ./bin/goose нет.
$(GOOSE_BIN):
	@echo ">> goose не найден, ставлю $(GOOSE_VERSION) в ./bin ..."
	@mkdir -p $(LOCAL_BIN)
	GOBIN=$(CURDIR)/$(LOCAL_BIN) go install github.com/pressly/goose/v3/cmd/goose@$(GOOSE_VERSION)
	@echo ">> готово: $(GOOSE_BIN)"

migrate: $(GOOSE_BIN) ## применить миграции к базе
	$(GOOSE) up

migrate-down: $(GOOSE_BIN) ## откатить последнюю миграцию
	$(GOOSE) down

migrate-status: $(GOOSE_BIN) ## показать статус миграций
	$(GOOSE) status

clean: ## удалить скачанные инструменты из ./bin
	rm -rf $(LOCAL_BIN)