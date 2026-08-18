-- +goose Up

CREATE TABLE context_processing_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id UUID NOT NULL
        REFERENCES professional_patient_connections (id) ON DELETE RESTRICT,
    requested_by_professional_user_id UUID NOT NULL
        REFERENCES professional_users (id) ON DELETE RESTRICT,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL DEFAULT 'processing',
    failure_code TEXT,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT context_processing_jobs_period_check CHECK (period_end > period_start),
    CONSTRAINT context_processing_jobs_period_length_check
        CHECK (period_end - period_start <= interval '31 days'),
    CONSTRAINT context_processing_jobs_status_check
        CHECK (status IN ('processing', 'completed', 'failed')),
    CONSTRAINT context_processing_jobs_failure_code_length_check
        CHECK (failure_code IS NULL OR char_length(btrim(failure_code)) BETWEEN 1 AND 100),
    CONSTRAINT context_processing_jobs_completion_check
        CHECK ((status = 'completed') = (completed_at IS NOT NULL))
);

CREATE UNIQUE INDEX context_processing_jobs_active_period_unique
    ON context_processing_jobs (connection_id, period_start, period_end)
    WHERE status = 'processing';

CREATE INDEX context_processing_jobs_connection_recent_idx
    ON context_processing_jobs (connection_id, created_at DESC);

CREATE TABLE context_summaries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id UUID NOT NULL
        REFERENCES professional_patient_connections (id) ON DELETE RESTRICT,
    processing_job_id UUID NOT NULL UNIQUE
        REFERENCES context_processing_jobs (id) ON DELETE RESTRICT,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    summary_ciphertext BYTEA NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    prompt_version TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT context_summaries_period_check CHECK (period_end > period_start),
    CONSTRAINT context_summaries_ciphertext_length_check
        CHECK (octet_length(summary_ciphertext) BETWEEN 1 AND 65536),
    CONSTRAINT context_summaries_provider_length_check
        CHECK (char_length(btrim(provider)) BETWEEN 1 AND 100),
    CONSTRAINT context_summaries_model_length_check
        CHECK (char_length(btrim(model)) BETWEEN 1 AND 160),
    CONSTRAINT context_summaries_prompt_version_length_check
        CHECK (char_length(btrim(prompt_version)) BETWEEN 1 AND 100)
);

CREATE INDEX context_summaries_connection_period_idx
    ON context_summaries (connection_id, period_end DESC);

CREATE TABLE context_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    context_summary_id UUID NOT NULL
        REFERENCES context_summaries (id) ON DELETE RESTRICT,
    kind TEXT NOT NULL,
    description_ciphertext BYTEA NOT NULL,
    confidence NUMERIC(4, 3),
    occurred_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT context_items_kind_check
        CHECK (kind IN ('event', 'theme', 'marked_topic')),
    CONSTRAINT context_items_description_length_check
        CHECK (octet_length(description_ciphertext) BETWEEN 1 AND 32768),
    CONSTRAINT context_items_confidence_check
        CHECK (confidence IS NULL OR confidence BETWEEN 0 AND 1)
);

CREATE INDEX context_items_summary_kind_idx
    ON context_items (context_summary_id, kind);

CREATE TABLE context_item_sources (
    context_item_id UUID NOT NULL REFERENCES context_items (id) ON DELETE RESTRICT,
    chat_message_id UUID NOT NULL REFERENCES chat_messages (id) ON DELETE RESTRICT,
    PRIMARY KEY (context_item_id, chat_message_id)
);

CREATE INDEX context_item_sources_message_idx
    ON context_item_sources (chat_message_id);

-- +goose Down

DROP TABLE IF EXISTS context_item_sources;
DROP TABLE IF EXISTS context_items;
DROP TABLE IF EXISTS context_summaries;
DROP TABLE IF EXISTS context_processing_jobs;
