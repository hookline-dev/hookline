# Чек-лист релиза v1.0.0

Пункт отмечается только после успешного прогона на commit, из которого создаётся
тег. Автоматическую часть запускает одна команда:

```bash
cp .env.example .env
make release-verify
```

## Автоматические проверки

- [ ] `make lint`, `make test`, `make test-integration` и `make cover` зелёные.
- [ ] Compose config, build и readiness проходят.
- [ ] Миграции проходят на пустой, повторно запущенной и legacy-базе без потери данных.
- [ ] HMAC, идемпотентность, fan-out, retry, DLQ, replay и circuit breaker покрыты тестами.
- [ ] Prometheus видит API и ровно три worker-реплики; Grafana загрузила Hookline dashboard.
- [ ] После `SIGKILL` worker-процессов leased message возвращается из `in_flight` и доставляется.
- [ ] Нагрузочный прогон: 500 событий за ≤60 секунд, ingest p95 ≤50 мс, `pending=0`, `in_flight=0`.
- [ ] Secret scan, container build и все обязательные GitHub checks зелёные.

## Ручные подтверждения

- [ ] Выполнен реальный GitHub → Hookline → Telegram прогон без секретов в логах.
- [ ] Записан GIF финального демо и добавлен в README.
- [ ] PR получил требуемые approvals; исключения из DoD явно записаны в release notes.
- [ ] В release notes сохранены commit SHA, окружение и строка `PASS` нагрузочного прогона.
- [ ] Создан аннотированный тег `v1.0.0` и опубликован GitHub Release.
