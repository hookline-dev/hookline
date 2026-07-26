# Hookline

Self-hosted сервис надёжной доставки вебхуков: принимает события, разносит подписчикам
по HTTP и гарантированно доставляет, переживая падения получателей.

Ретраи с экспоненциальным backoff и джиттером · dead-letter queue с ручным replay ·
circuit breaker · HMAC-подпись запросов · очередь на PostgreSQL (`FOR UPDATE SKIP LOCKED`)
с несколькими параллельными воркерами.

> ⚠️ Учебный проект. В активной разработке.

## Быстрый старт

```bash
git clone https://github.com/hookline-dev/hookline && cd hookline
cp .env.example .env
make up && make migrate && make demo
```

Дашборд: http://localhost:8080

Подробнее — [Онбординг за 30 минут](docs/onboarding.md).

## Как это работает

<!-- сюда позже  вставим схему потока и гифку демо -->

## Документация

**Новичку — читать в этом порядке:**

| Документ | О чём |
|---|---|
| 1. [За что взяться и в каком порядке](docs/NEWCOMER_PATH.md) | **старт для нового участника**: все задачи по этапам |
| 2. [Git с нуля](docs/GIT_FOR_BEGINNERS.md) | для тех, кто впервые на GitHub |
| 3. [Онбординг](docs/onboarding.md) | поднять проект локально |
| 4. [Инструкции по задачам](docs/TASK_GUIDES.md) | пошагово: как делать конкретную задачу |
| 5. [Руководства: backoff, matcher, signing, sink](docs/TASK_GUIDES_newbie.md) | подробно по четырём задачам новичка |

**Остальное:**

| Документ | О чём |
|---|---|
| [Техническое задание](docs/TZ.md) | что строим, архитектура, спецификации модулей |
| [Роадмап](docs/ROADMAP.md) | план на 8 недель по фазам |
| [CONTRIBUTING](CONTRIBUTING.md) | правила разработки — прочитать до первого PR |
| [Работа с доской](docs/GITHUB_PROJECTS_GUIDE.md) | GitHub Projects + скрам-минимум |
| [Каталог задач для новичков](docs/STARTER_TASKS.md) | справочник задач T-01…T-19 (для лида) |
| [Настройка репозитория](docs/REPO_SETUP.md) | как всё устроено (для лида) |
| [Команда](docs/team.md) | кто за что отвечает |

## Доска задач

Все задачи ведём на доске проекта: **[Projects](https://github.com/hookline-dev/hookline/projects)**.
Как ей пользоваться — в [гайде по доске](docs/GITHUB_PROJECTS_GUIDE.md).

## Команда

См. [docs/team.md](docs/team.md).

## Лицензия

[MIT](LICENSE)