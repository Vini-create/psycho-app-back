-- +goose Up

ALTER TABLE chat_messages
    ADD COLUMN generation_attempted_at TIMESTAMPTZ;

UPDATE chat_messages
SET generation_attempted_at = created_at
WHERE role = 'user';

ALTER TABLE chat_messages
    ADD CONSTRAINT chat_messages_generation_attempt_role_check
        CHECK ((role = 'user') = (generation_attempted_at IS NOT NULL)),
    ADD CONSTRAINT chat_messages_generation_attempt_time_check
        CHECK (generation_attempted_at IS NULL OR generation_attempted_at >= created_at);

-- +goose Down

ALTER TABLE chat_messages
    DROP CONSTRAINT IF EXISTS chat_messages_generation_attempt_time_check,
    DROP CONSTRAINT IF EXISTS chat_messages_generation_attempt_role_check,
    DROP COLUMN IF EXISTS generation_attempted_at;
