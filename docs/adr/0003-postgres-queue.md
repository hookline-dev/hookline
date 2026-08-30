# ADR 0003: PostgreSQL queue

Queue хранится в `messages`, claim использует `FOR UPDATE SKIP LOCKED`.
Это сохраняет атомарность fan-out без отдельного брокера. Lease + reaper дают
at-least-once.
