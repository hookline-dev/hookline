# Delivery specification

Event и messages фиксируются атомарно, затем доставляются асинхронно с
семантикой at-least-once. Порядок не гарантируется. `2xx` — успех; остальные
ответы повторяются с exponential backoff + full jitter. После лимита попыток
сообщение становится `dead`; Replay создаёт новое сообщение с `replay_of`.

Подписывается `<unix>.<raw body>` через HMAC-SHA256. Lease/reaper возвращает
сообщение после аварии worker, поэтому дубль между HTTP-обработкой и Ack возможен.
