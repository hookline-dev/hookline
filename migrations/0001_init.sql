-- +goose Up

-- apps

CREATE TABLE apps (
    id          uuid PRIMARY KEY,
    name        text NOT NULL,
    api_key_hash text NOT NULL,          -- хэш ключа доступа
    created_at  timestamptz NOT NULL DEFAULT now()
);


-- endpoints

CREATE TABLE endpoints (
    id           uuid PRIMARY KEY,
    app_id       uuid NOT NULL REFERENCES apps(id),
    url          text NOT NULL,
    secret       text NOT NULL,               -- общий секрет HMAC
    status       text NOT NULL DEFAULT 'active'
                 CHECK (status IN ('active','disabled')),

    -- поля circuit breaker
    breaker_state          text NOT NULL DEFAULT 'closed'
                           CHECK (breaker_state IN ('closed','open','half_open')),
    breaker_failures       int  NOT NULL DEFAULT 0,
    breaker_opened_at      timestamptz,
    breaker_open_duration  interval NOT NULL DEFAULT '5 minutes',

    rate_limit_rps int NOT NULL DEFAULT 5,
    created_at     timestamptz NOT NULL DEFAULT now()
);


-- subscriptions

CREATE TABLE subscriptions (
    id          uuid PRIMARY KEY,
    endpoint_id uuid NOT NULL REFERENCES endpoints(id),
    event_type  text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (endpoint_id, event_type)
);


-- events

CREATE TABLE events (
    id          uuid PRIMARY KEY,
    app_id      uuid NOT NULL REFERENCES apps(id),
    event_type  text NOT NULL,
    payload     jsonb NOT NULL,
    idem_key    text,
    received_at timestamptz NOT NULL DEFAULT now(),

    UNIQUE (app_id, idem_key)          -- уникальный ключ внутри приложения
);

CREATE INDEX idx_events_app_received ON events (app_id, received_at DESC);


-- messages
CREATE TABLE messages (
    id          uuid PRIMARY KEY,
    event_id    uuid NOT NULL REFERENCES events(id),
    endpoint_id uuid NOT NULL REFERENCES endpoints(id),

    status      text NOT NULL DEFAULT 'pending'
                CHECK (status IN ('pending','in_flight','delivered','dead')),

    attempt         int NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL,
    locked_until    timestamptz,
    locked_by       text,

    replay_of   uuid REFERENCES messages(id),   -- если ручной повтор

    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_messages_due
    ON messages (next_attempt_at)
    WHERE status = 'pending';
CREATE INDEX idx_messages_stuck
    ON messages (locked_until)
    WHERE status = 'in_flight';
CREATE INDEX idx_messages_endpoint ON messages (endpoint_id, created_at DESC);


-- attempts
CREATE TABLE attempts (
    id          bigserial PRIMARY KEY,
    message_id  uuid NOT NULL REFERENCES messages(id),
    attempt_no  int NOT NULL,

    request_headers  jsonb,
    response_code    int,
    response_snippet text,
    error            text,
    duration_ms      int NOT NULL,

    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_attempts_message ON attempts (message_id, attempt_no);


-- +goose Down
DROP TABLE IF EXISTS attempts;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS subscriptions;
DROP TABLE IF EXISTS endpoints;
DROP TABLE IF EXISTS apps;
