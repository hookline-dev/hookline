# ============================================================================
# Команды разработки.
#
# make help   — список всех команд (надо сделать issue #28)
# ============================================================================


# --- настройки -------------------------------------------------------------

# Версия линтера
GOLANGCI_VERSION ?= v2.12.2

# Локальные инструменты в ./bin внутри проекта
LOCAL_BIN := bin
GOLANGCI  := $(LOCAL_BIN)/golangci-lint

# Подключаем .env, чтобы переменные были доступны и в make, и в дочерних
# командах (например, `make run` увидит DATABASE_URL).
ifneq (,$(wildcard .env))
    include .env
    export
endif
# Если написать просто `make` — покажем справку, а не запустим первую цель.
.DEFAULT_GOAL := help
# .PHONY перечислит цели, которые НЕ являются именами файлов.
.PHONY: help up down down-v logs ps psql run test test-short fmt vet tidy \
        lint tools migrate clean check


# --- справка ---------------------------------------------------------------

help:


# --- инфраструктура (docker) -----------------------------------------------

up: ## поднять базу данных и дождаться её готовности
	docker compose up -d --wait
	@echo ">> база готова: localhost:$(POSTGRES_PORT)"
 
down: ## погасить контейнеры (данные базы сохранятся)
	docker compose down
 
down-v: ## погасить контейнеры И УДАЛИТЬ данные базы (полный сброс)
	docker compose down -v
 
logs: ## смотреть логи контейнеров (Ctrl+C — выйти)
	docker compose logs -f
 
ps: ## показать, какие контейнеры запущены
	docker compose ps
 
psql: ## открыть консоль psql внутри контейнера с базой
	docker compose exec postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB)
 


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
 
lint: $(GOLANGCI) ## проверить код линтером (то же самое, что делает CI)
	./$(GOLANGCI) run ./...
 
lint-fix: $(GOLANGCI) ##линтер + автоисправление того, что можно починить само
	$(GOLANGCI) run --fix ./...
 
# --- прочее ----------------------------------------------------------------

migrate: ## применить миграции к базе (появится вместе с задачей #10)
	@echo "TODO: подключить goose. Пока миграций нет — задача #10 на доске."

clean: ## удалить скачанные инструменты из ./bin
	rm -rf $(LOCAL_BIN)