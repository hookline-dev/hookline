# Техническое задание: Hookline — сервис надёжной доставки вебхуков

Версия: 1.0 · Срок: 8 недель · Команда: 5 человек · Статус: вариант на выбор команды

---

## 1. Что мы строим

**Hookline** — self-hosted сервис, который принимает события от источников и **гарантированно доставляет** их подписчикам по HTTP, переживая падения получателей.

Аналоги в индустрии: Svix, Hookdeck, Convoy, внутренние webhook-платформы Stripe/GitHub. Мы делаем их ядро.

Поток данных:

```
Источник события            Hookline                         Подписчик
(GitHub webhook,   ──POST──▶ приём + подпись источника ──▶ очередь (Postgres)
 наш API, curl)              │                                  │
                             │                             воркер-пул: берёт due-задачи
                             │                             (FOR UPDATE SKIP LOCKED)
                             │                                  │
                             │                             POST на endpoint подписчика,
                             │                             HMAC-подпись, таймаут
                             │                                  │
                             │                        2xx? ──да──▶ delivered
                             │                                  │нет
                             │                        retry с backoff+jitter (до N раз)
                             │                                  │исчерпано
                             ▼                                  ▼
                        Dashboard: события,               dead letter queue
                        попытки, ручной replay            (+ ручной replay)
```

Демо-сценарий проекта (dogfooding): наш собственный GitHub-репозиторий шлёт вебхуки о пушах в Hookline → Hookline доставляет их в наш сервис-приёмник и в Telegram-бота. Пуш коммита виден в чате через нашу же систему.

## 2. Цели

**Продуктовая:** рабочий сервис, который можно поднять `docker compose up`, зарегистрировать приложение и подписки, направить в него реальные вебхуки GitHub и наблюдать доставку в дашборде.

**Учебная (главная):** каждый участник выносит из проекта набор тем, по которым сможет говорить на собеседовании 20 минут без запинки (§9).

## 3. Роли

| # | Роль | Уровень | Владеет | Обязанности |
|---|------|---------|---------|-------------|
| 1 | **Лид** (ты) | джун+/нач.мидл | `internal/queue`, `internal/worker`, инфраструктура, CI | Ядро: очередь на SKIP LOCKED, воркер-пул, планировщик ретраев, graceful shutdown, horizontal scaling. Финальное ревью и мерж, ADR, нарезка задач |
| 2 | **Бэкендер-стажёр** | стажёр | `internal/ingest`, `internal/delivery`, circuit breaker | Приём событий (валидация, дедуп по idempotency-key), HTTP-доставка (таймауты, ограничение размера ответа, обработка кодов), circuit breaker на «мёртвые» эндпоинты. Второй ревьюер L0/L1 |
| 3 | **Разработчик A** | до горутин | `internal/signing`, `internal/backoff`, `internal/matcher` | Чистая логика без I/O, всё под 100% тестов: HMAC-подпись и верификация (схема Stripe-like), калькулятор backoff с джиттером, матчер подписок по типам событий (`order.*`, `push`). Позже — L3: воркер очистки старых событий |
| 4 | **Разработчик B** | до горутин | `internal/api`, DTO, middleware, OpenAPI, dashboard | Admin API (приложения, эндпоинты, подписки, ключи), auth, единый формат ошибок, OpenAPI. Дашборд (простые страницы) |
| 5 | **Разработчик C** | до горутин | `internal/attempts`, `cmd/sink`, seed, docs, метрики | Журнал попыток и API их просмотра с пагинацией; сервис-приёмник `sink` для демо и тестов (умеет отвечать 200/500/таймаутом по команде); seed; метрики; документация |

Правила ролей — как в CONTRIBUTING (владелец = дефолтный ревьюер, мержит лид, лестница L0→L3, при пропаже на 2 недели модуль переходит).

## 4. Стек

| Слой | Выбор |
|---|---|
| Язык | **Go 1.26** (зафиксирован в go.mod и CI) |
| HTTP | `net/http` + `chi`. Фреймворков нет |
| Хранилище/очередь | **PostgreSQL 16 и только он.** Очередь строим на таблице + `FOR UPDATE SKIP LOCKED`. Kafka/RabbitMQ/Redis запрещены до фазы 5 — иначе теряется весь смысл упражнения |
| Драйвер | `pgx/v5` (+ `sqlc` по ADR-0002) |
| Миграции | `goose` |
| Внешний источник | **GitHub Webhooks** (бесплатно, без ключей) — наш демо-источник. Также любой `curl` и наш `cmd/sink` |
| Уведомления в демо | Telegram Bot API (бесплатно) как один из подписчиков |
| Конфиг/логи/метрики | env, `log/slog`, Prometheus + Grafana |
| Тесты | table-driven + testify; интеграционные — testcontainers; HTTP-моки — `httptest` |
| Линтер | golangci-lint **v2** (v2.12.x) |
| CI | GitHub Actions, репозиторий публичный (Actions для публичных репо бесплатны и без лимита минут) |
| Локально | compose: postgres, **3 реплики hookline-worker**, hookline-api, sink, grafana |
| Фронт | Минимальный: 4 страницы (приложения/эндпоинты, лента событий, детали события с таймлайном попыток и кнопкой Replay, страница верификации подписи). Ванильный JS/HTMX, ≤15% усилий |

## 5. Архитектура

### 5.1 Структура

```
hookline/
├── cmd/
│   ├── hookline/main.go       # api + worker в одном бинаре, режимы через флаг --mode=api|worker|all
│   └── sink/main.go           # тестовый приёмник (управляемо отвечает 200/4xx/5xx/таймаутом)
├── internal/
│   ├── domain/                # Event, Message, Attempt, Endpoint, Subscription, ошибки
│   ├── ingest/                # приём событий: валидация, idempotency, фан-аут в сообщения
│   ├── queue/                 # ЯДРО: claim/ack/nack, SKIP LOCKED, видимость, лизы
│   ├── worker/                # пул воркеров, планировщик, graceful shutdown
│   ├── delivery/              # HTTP-клиент доставки: таймауты, лимиты, коды
│   ├── backoff/               # чистая функция: экспонента + full jitter
│   ├── signing/               # HMAC-SHA256 подпись/верификация, защита от replay
│   ├── matcher/               # матчинг event_type ↔ подписки (wildcard)
│   ├── breaker/               # circuit breaker по эндпоинту
│   ├── attempts/              # журнал попыток, выборки
│   ├── api/                   # admin API + ingest API + dashboard handlers
│   └── storage/postgres/
├── migrations/  web/  docs/(adr, api/openapi.yaml, onboarding.md, delivery-spec.md)
├── deploy/docker-compose.yml
└── .github/ .golangci.yml Makefile CONTRIBUTING.md README.md
```

### 5.2 Модель данных

```sql
apps(id uuid pk, name text, created_at)
endpoints(
  id uuid pk, app_id fk, url text, secret text,       -- секрет для HMAC подписчика
  status text,                        -- active|disabled|circuit_open
  rate_limit_rps int default 5,
  created_at
)
subscriptions(id uuid pk, endpoint_id fk, event_type text)  -- 'push', 'order.*', '*'

events(                                -- то, что пришло ИЗВНЕ, один раз
  id uuid pk, app_id fk, event_type text,
  payload jsonb, idem_key text unique,
  received_at
)
messages(                              -- ЕДИНИЦА ДОСТАВКИ: событие × эндпоинт. Это и есть очередь
  id uuid pk, event_id fk, endpoint_id fk,
  status text,                         -- pending|in_flight|delivered|failed|dead
  attempt int default 0,
  next_attempt_at timestamptz not null,-- когда становится «due»
  locked_until timestamptz null,       -- лиз воркера (защита от зависших)
  locked_by text null,                 -- id воркера (для наблюдаемости)
  created_at, updated_at
)
create index on messages (status, next_attempt_at);   -- индекс под claim-запрос

attempts(                              -- аудит каждой попытки
  id bigserial pk, message_id fk, attempt int,
  request_headers jsonb, response_code int null,
  response_body_snippet text,          -- обрезаем до 1KB
  error text null, duration_ms int, created_at
)
```

### 5.3 Ядро: очередь на SKIP LOCKED (ADR-0003, главный документ проекта)

Claim-запрос воркера (упрощённо):

```sql
UPDATE messages SET
  status = 'in_flight',
  locked_until = now() + interval '30 seconds',
  locked_by = $1
WHERE id IN (
  SELECT id FROM messages
  WHERE status = 'pending' AND next_attempt_at <= now()
  ORDER BY next_attempt_at
  FOR UPDATE SKIP LOCKED
  LIMIT $2
)
RETURNING *;
```

Инварианты (нарушение = блокер на ревью):
1. Одно сообщение обрабатывается одним воркером в момент времени — это обеспечивает `SKIP LOCKED` + лиз `locked_until`.
2. Воркер, потерявший связь/упавший, не блокирует сообщение навсегда: по истечении `locked_until` сообщение снова становится доступным (**reaper**-запрос возвращает зависшие `in_flight` в `pending`).
3. Семантика — **at-least-once**: получатель обязан быть идемпотентным, мы шлём `X-Hookline-Message-Id` и документируем это. Exactly-once не обещаем и на собеседовании объясняем почему.
4. Ack (успех) и nack (перенос с backoff) — атомарные UPDATE вместе с записью в `attempts` в одной транзакции БД.
5. Работоспособность при N репликах: гоняем 3 воркер-контейнера, каждое сообщение доставлено ровно один раз при отсутствии таймаутов (проверяется интеграционным тестом).

### 5.4 Ретраи и backoff (модуль backoff, чистые функции)

- Формула: `delay = min(cap, base * 2^attempt)`, затем **full jitter**: `rand(0, delay)`.
- Дефолт: base 5s, cap 6h, max 8 попыток (конфиг). После исчерпания → `status = dead`, сообщение уходит в DLQ-вью, эндпоинт помечается проблемным.
- Почему jitter обязателен: без него все сообщения к упавшему эндпоинту ретраятся синхронно и добивают его при подъёме (thundering herd). Это объяснение должно быть в `docs/delivery-spec.md` — оно же ваш ответ на собеседовании.
- Мгновенный ретрай запрещён; первая попытка — сразу при создании сообщения.

### 5.5 Circuit breaker (стажёр)

Три состояния на эндпоинт: `closed → open → half-open`. N подряд неуспешных доставок (конфиг, дефолт 10) → `open` на T минут: сообщения не отправляются, а переносятся. По истечении T — `half-open`: пропускаем одно пробное сообщение; успех → `closed`, провал → снова `open` с удвоенным T. Состояние — в таблице endpoints (не в памяти: воркеров несколько).

### 5.6 Подпись (модуль signing, разработчик A)

- Заголовки исходящего запроса: `X-Hookline-Id`, `X-Hookline-Timestamp`, `X-Hookline-Signature: v1=<hex(HMAC-SHA256(secret, timestamp + "." + body))>`.
- Верификация (для подписчиков и для нашей страницы-проверялки): сравнение **constant-time** (`hmac.Equal`), отклонение при расхождении timestamp > 5 минут (защита от replay).
- Входящие вебхуки GitHub верифицируются симметрично по их схеме `X-Hub-Signature-256` — тем же модулем.
- Требования: ноль зависимостей от БД/HTTP, покрытие 100%, зафиксированные тестовые векторы (не менять никогда).

### 5.7 API

```
# Ingest (публичный, для источников)
POST /ingest/{app_id}                 заголовок Idempotency-Key; тело — любой JSON
POST /ingest/github/{app_id}          верифицирует X-Hub-Signature-256, маппит X-GitHub-Event → event_type

# Admin (под auth)
POST/GET     /api/v1/apps
POST/GET/DEL /api/v1/apps/{id}/endpoints        {url, secret, rateLimit}
POST/DEL     /api/v1/endpoints/{id}/subscriptions {eventType}
GET          /api/v1/events?type&from&limit&cursor
GET          /api/v1/messages?status&endpoint&limit&cursor      (в т.ч. status=dead → DLQ)
GET          /api/v1/messages/{id}              событие + таймлайн всех попыток
POST         /api/v1/messages/{id}/replay       ручной перезапуск (новое сообщение, ссылка на исходное)
POST         /api/v1/endpoints/{id}/reset-breaker
GET          /healthz /readyz /metrics
```

Auth: простые API-ключи (`Authorization: Bearer <key>`), хэш в БД. JWT здесь избыточен — и это тоже осознанное решение в ADR.

### 5.8 Дашборд (≤15% усилий)

4 страницы: (1) приложения/эндпоинты/подписки; (2) лента событий с фильтром; (3) детали сообщения — таймлайн попыток (код ответа, длительность, тело-снипет) и кнопка Replay; (4) проверялка подписи (вставь payload + secret + signature → «валидна/нет»). Плюс маленький блок метрик: pending/in_flight/dead.

## 6. Нефункциональные требования

| Требование | Значение |
|---|---|
| Пропускная способность | 500 сообщений/мин на 3 воркерах локально, без роста pending |
| Latency ingest | p95 < 50 мс (ingest только пишет в БД, доставка асинхронна — это принципиально) |
| Доставка | at-least-once; дубликатов не больше, чем при таймаутах; порядок не гарантируется (документируем!) |
| Устойчивость | kill -9 воркера в момент доставки → сообщение переедет к другому после истечения лиза, не потеряется |
| Тайминги доставки | таймаут HTTP-запроса 10 с, чтение ответа ограничено 1 МБ |
| Race | `go test -race ./...` чист; тест «3 воркера, 200 сообщений, каждое доставлено ровно раз» |
| Секреты | секреты эндпоинтов не логируются и не отдаются через API после создания |
| Покрытие | `signing`, `backoff`, `matcher` = 100%; `queue`, `worker` ≥ 85%; общее ≥ 60% |

## 7. Definition of Done — как в CONTRIBUTING (тесты, линтер, PR ≤ 300 строк, ревью; `queue`/`worker`/`signing` — 2 аппрува).

## 8. Риски

| Риск | Митигция |
|---|---|
| Очередь спроектирована неверно и всё придётся переделывать | ADR-0003 и схема `messages` пишутся и ревьюются на неделе 1 **до** любого кода доставки; ключевой тест (3 воркера × 200 сообщений) пишется вместе с очередью, а не после |
| Новичкам не за что взяться, пока нет ядра | Их модули (signing, backoff, matcher, sink, admin API) **не зависят от очереди** и пишутся параллельно с первого дня |
| Тесты «на время» флакуют | Время инжектится интерфейсом `Clock`; в тестах — фейковые часы. Правило с недели 1: `time.Now()` в бизнес-логике запрещён |
| Отвал участников | к неделе 5 при ≤3 людях: режем circuit breaker, rate-limit и Telegram; ядро (ingest+queue+worker+retry+DLQ+дашборд) остаётся — это уже полноценный проект |
| Проект «не видно» | Dogfooding-демо с GitHub → Telegram делаем в фазе 3, гифка идёт в README |

## 9. Что вы сможете рассказать на собеседовании (карта «фича → вопрос»)

| Что сделали | Вопрос, на который теперь есть свой ответ |
|---|---|
| Очередь на `FOR UPDATE SKIP LOCKED` | «Как сделать очередь без Kafka? Почему не `SELECT ... FOR UPDATE`? Что будет при N воркерах?» |
| Лиз `locked_until` + reaper | «Воркер упал посреди обработки — что происходит с задачей?» |
| Idempotency-Key на ingest + `X-Hookline-Message-Id` на доставке | «At-least-once vs exactly-once. Как получателю защититься от дублей?» |
| Экспоненциальный backoff + full jitter | «Зачем джиттер? Что такое thundering herd / retry storm?» |
| DLQ + ручной replay | «Что делать с сообщениями, которые не доставились никогда?» |
| Circuit breaker | «Как перестать добивать лежачий сервис? Что такое half-open?» |
| HMAC-подпись + timestamp | «Как получатель убедится, что вебхук от вас? Что такое replay-атака и constant-time сравнение?» |
| Разделение ingest и delivery | «Почему приём быстрый, а доставка асинхронная? Что такое backpressure?» |
| Graceful shutdown воркеров | «SIGTERM пришёл в момент доставки — что делаете?» |
| `Clock`-интерфейс и фейковое время | «Как тестируете код, зависящий от времени?» |
| Метрики pending/in_flight/dead, возраст самого старого pending | «Как понять, что система деградирует, раньше пользователей?» |

Каждый участник к финалу обязан уметь объяснить **свои** строки этой таблицы у доски. Это не бонус, это часть Definition of Done проекта.

## 10. Релиз v1.0 (8 недель)

Ingest (+GitHub-совместимый), фан-аут по подпискам, очередь на SKIP LOCKED с 3 воркерами, HMAC-подпись, ретраи с backoff+jitter, DLQ + replay, circuit breaker, журнал попыток, дашборд, метрики Grafana, sink-сервис, compose одной командой, dogfooding-демо (пуш → Telegram), публичный репо, зелёный CI, README с гифкой, тег v1.0.0. У каждого участника: ≥ 8 смерженных PR, ≥ 1 конкурентная задача, свой абзац в docs/team.md и свои строки в таблице §9.
