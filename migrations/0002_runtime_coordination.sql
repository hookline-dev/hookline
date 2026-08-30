-- +goose Up
ALTER TABLE apps ADD COLUMN IF NOT EXISTS github_webhook_secret text;
UPDATE apps SET github_webhook_secret='ghwh_'||replace(gen_random_uuid()::text,'-','') WHERE github_webhook_secret IS NULL;
ALTER TABLE apps ALTER COLUMN github_webhook_secret SET NOT NULL;
ALTER TABLE endpoints ADD COLUMN IF NOT EXISTS breaker_probe_in_flight boolean NOT NULL DEFAULT false;
ALTER TABLE endpoints ADD COLUMN IF NOT EXISTS rate_window_started_at timestamptz;
ALTER TABLE endpoints ADD COLUMN IF NOT EXISTS rate_window_count int NOT NULL DEFAULT 0;
DROP INDEX IF EXISTS idx_attempts_message;
CREATE UNIQUE INDEX idx_attempts_message ON attempts(message_id,attempt_no);
-- +goose Down
DROP INDEX IF EXISTS idx_attempts_message;
CREATE INDEX idx_attempts_message ON attempts(message_id,attempt_no);
ALTER TABLE endpoints DROP COLUMN IF EXISTS rate_window_count;
ALTER TABLE endpoints DROP COLUMN IF EXISTS rate_window_started_at;
ALTER TABLE endpoints DROP COLUMN IF EXISTS breaker_probe_in_flight;
ALTER TABLE apps DROP COLUMN IF EXISTS github_webhook_secret;
