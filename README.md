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

## Как это работает

<!-- сюда потом вставим схему потока и гифку демо -->

## Документация

| Документ | О чём |
|---|---|
| [Техническое задание](docs/TZ.md) | что строим, архитектура, требования |
| [Роадмап](docs/ROADMAP.md) | план на 8 недель по фазам |
| [CONTRIBUTING](CONTRIBUTING.md) | правила разработки — прочитать до первого PR |
| [Работа с доской](docs/GITHUB_PROJECTS_GUIDE.md) | GitHub Projects + скрам-минимум |
| [Git с нуля](docs/GIT_FOR_BEGINNERS.md) | для тех, кто впервые на GitHub |
| [Задачи для новичков](docs/STARTER_TASKS.md) | с чего начать |
| [Онбординг](docs/onboarding.md) | поднять проект локально за 30 минут |

## Команда
См. [docs/team.md](docs/team.md)

## Лицензия
MIT