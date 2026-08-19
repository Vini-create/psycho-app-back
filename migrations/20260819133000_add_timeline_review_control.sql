-- +goose Up

ALTER TABLE context_timeline_entries
    ADD COLUMN included BOOLEAN NOT NULL DEFAULT true;

-- +goose Down

ALTER TABLE context_timeline_entries
    DROP COLUMN IF EXISTS included;
