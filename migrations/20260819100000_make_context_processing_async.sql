-- +goose Up

ALTER TABLE context_processing_jobs
    DROP CONSTRAINT context_processing_jobs_status_check;

UPDATE context_processing_jobs
SET status = 'queued'
WHERE status = 'processing';

ALTER TABLE context_processing_jobs
    ADD COLUMN available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN claimed_at TIMESTAMPTZ,
    ADD COLUMN attempt_count INTEGER NOT NULL DEFAULT 0,
    ADD CONSTRAINT context_processing_jobs_status_check
        CHECK (status IN ('queued', 'processing', 'completed', 'failed')),
    ADD CONSTRAINT context_processing_jobs_attempt_count_check
        CHECK (attempt_count >= 0),
    ADD CONSTRAINT context_processing_jobs_claim_check
        CHECK ((status = 'processing') = (claimed_at IS NOT NULL));

DROP INDEX context_processing_jobs_active_period_unique;

CREATE UNIQUE INDEX context_processing_jobs_open_period_unique
    ON context_processing_jobs (connection_id, period_start, period_end)
    WHERE status IN ('queued', 'processing');

CREATE INDEX context_processing_jobs_worker_queue_idx
    ON context_processing_jobs (available_at, created_at)
    WHERE status = 'queued';

-- +goose Down

DROP INDEX IF EXISTS context_processing_jobs_worker_queue_idx;
DROP INDEX IF EXISTS context_processing_jobs_open_period_unique;

CREATE UNIQUE INDEX context_processing_jobs_active_period_unique
    ON context_processing_jobs (connection_id, period_start, period_end)
    WHERE status = 'processing';

ALTER TABLE context_processing_jobs
    DROP CONSTRAINT IF EXISTS context_processing_jobs_claim_check,
    DROP CONSTRAINT IF EXISTS context_processing_jobs_attempt_count_check,
    DROP CONSTRAINT IF EXISTS context_processing_jobs_status_check,
    DROP COLUMN IF EXISTS attempt_count,
    DROP COLUMN IF EXISTS claimed_at,
    DROP COLUMN IF EXISTS available_at,
    ADD CONSTRAINT context_processing_jobs_status_check
        CHECK (status IN ('processing', 'completed', 'failed'));
