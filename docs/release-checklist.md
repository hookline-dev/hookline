# Чек-лист релиза v1.0.0

- [ ] `make lint`, `make test` и `make test-integration` проходят.
- [ ] Compose smoke-test и secret scan зелёные в CI.
- [ ] Миграции проходят на пустой и уже созданной базе.
- [ ] Проверены HMAC, идемпотентность, retry, DLQ, replay и circuit breaker.
- [ ] Нагрузочный прогон из `docs/performance.md` соответствует критериям.
- [ ] Prometheus видит API и все worker-реплики; Grafana отображает очередь и доставки.
- [ ] Выполнен реальный GitHub → Hookline → Telegram прогон без секретов в логах.
- [ ] Записан GIF финального демо и добавлен в README.
- [ ] PR получил обязательное одобрение владельца кода.
- [ ] Создан аннотированный тег `v1.0.0` и опубликованы release notes.
