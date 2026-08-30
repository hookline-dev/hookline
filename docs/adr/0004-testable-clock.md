# ADR 0004: Explicit Clock

Бизнес-логика получает `domain.Clock`; системное время остаётся runtime-деталью.
Retry, подписи, breaker и lease тестируются детерминированно.
