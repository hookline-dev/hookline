// Package queue реализует очередь сообщений поверх таблицы messages
// с захватом задач через FOR UPDATE SKIP LOCKED. См. docs/TZ.md §6.5.
// ЯДРО: claim / ack / nack / reap
package queue
