-- +goose Up
ALTER TABLE endpoints
    ADD CONSTRAINT endpoints_rate_limit_rps_non_negative CHECK (rate_limit_rps >= 0),
    ADD CONSTRAINT endpoints_breaker_failures_non_negative CHECK (breaker_failures >= 0);
CREATE UNIQUE INDEX IF NOT EXISTS idx_apps_api_key_hash ON apps(api_key_hash);

-- +goose Down
DROP INDEX IF EXISTS idx_apps_api_key_hash;
ALTER TABLE endpoints
    DROP CONSTRAINT IF EXISTS endpoints_breaker_failures_non_negative,
    DROP CONSTRAINT IF EXISTS endpoints_rate_limit_rps_non_negative;
