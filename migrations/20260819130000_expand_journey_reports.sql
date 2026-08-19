-- +goose Up

ALTER TABLE context_summaries
    ADD COLUMN schema_version TEXT NOT NULL DEFAULT 'journey-report-v1',
    ADD COLUMN title_ciphertext BYTEA,
    ADD COLUMN coverage_conversation_count INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN coverage_user_message_count INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN coverage_active_day_count INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN coverage_completeness TEXT NOT NULL DEFAULT 'limited',
    ADD COLUMN coverage_note_ciphertext BYTEA,
    ADD COLUMN limitations_ciphertext BYTEA,
    ADD COLUMN graph_version TEXT NOT NULL DEFAULT 'legacy',
    ADD COLUMN review_status TEXT NOT NULL DEFAULT 'approved',
    ADD COLUMN reviewed_at TIMESTAMPTZ;

UPDATE context_summaries
SET title_ciphertext = summary_ciphertext,
    coverage_note_ciphertext = summary_ciphertext,
    reviewed_at = created_at;

ALTER TABLE context_summaries
    ALTER COLUMN title_ciphertext SET NOT NULL,
    ALTER COLUMN coverage_note_ciphertext SET NOT NULL,
    ADD CONSTRAINT context_summaries_schema_version_length_check
        CHECK (char_length(btrim(schema_version)) BETWEEN 1 AND 100),
    ADD CONSTRAINT context_summaries_title_ciphertext_length_check
        CHECK (octet_length(title_ciphertext) BETWEEN 1 AND 8192),
    ADD CONSTRAINT context_summaries_coverage_count_check
        CHECK (
            coverage_conversation_count BETWEEN 1 AND 500
            AND coverage_user_message_count BETWEEN 1 AND 500
            AND coverage_active_day_count BETWEEN 1 AND 31
        ),
    ADD CONSTRAINT context_summaries_coverage_completeness_check
        CHECK (coverage_completeness IN ('limited', 'partial', 'substantial')),
    ADD CONSTRAINT context_summaries_coverage_note_ciphertext_length_check
        CHECK (octet_length(coverage_note_ciphertext) BETWEEN 1 AND 8192),
    ADD CONSTRAINT context_summaries_limitations_ciphertext_length_check
        CHECK (
            limitations_ciphertext IS NULL
            OR octet_length(limitations_ciphertext) BETWEEN 1 AND 32768
        ),
    ADD CONSTRAINT context_summaries_graph_version_length_check
        CHECK (char_length(btrim(graph_version)) BETWEEN 1 AND 100),
    ADD CONSTRAINT context_summaries_review_status_check
        CHECK (review_status IN ('pending_review', 'approved', 'rejected')),
    ADD CONSTRAINT context_summaries_reviewed_at_check
        CHECK ((review_status = 'pending_review') = (reviewed_at IS NULL));

CREATE INDEX context_summaries_pending_review_idx
    ON context_summaries (connection_id, created_at DESC)
    WHERE review_status = 'pending_review';

ALTER TABLE context_items
    DROP CONSTRAINT context_items_kind_check,
    ADD COLUMN title_ciphertext BYTEA,
    ADD COLUMN impact_ciphertext BYTEA,
    ADD COLUMN evidence_strength TEXT NOT NULL DEFAULT 'uncertain',
    ADD COLUMN limitations_ciphertext BYTEA,
    ADD COLUMN included BOOLEAN NOT NULL DEFAULT true;

UPDATE context_items
SET title_ciphertext = description_ciphertext;

ALTER TABLE context_items
    ALTER COLUMN title_ciphertext SET NOT NULL,
    ADD CONSTRAINT context_items_kind_check
        CHECK (
            kind IN (
                'priority', 'event', 'challenge', 'emotion', 'thought', 'behavior',
                'strategy', 'support', 'change', 'open_topic', 'safety_context',
                'theme', 'marked_topic'
            )
        ),
    ADD CONSTRAINT context_items_title_ciphertext_length_check
        CHECK (octet_length(title_ciphertext) BETWEEN 1 AND 8192),
    ADD CONSTRAINT context_items_impact_ciphertext_length_check
        CHECK (
            impact_ciphertext IS NULL
            OR octet_length(impact_ciphertext) BETWEEN 1 AND 16384
        ),
    ADD CONSTRAINT context_items_evidence_strength_check
        CHECK (
            evidence_strength IN (
                'explicit_once', 'explicit_repeated', 'uncertain', 'contradictory'
            )
        ),
    ADD CONSTRAINT context_items_limitations_ciphertext_length_check
        CHECK (
            limitations_ciphertext IS NULL
            OR octet_length(limitations_ciphertext) BETWEEN 1 AND 32768
        );

CREATE TABLE context_timeline_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    context_summary_id UUID NOT NULL
        REFERENCES context_summaries (id) ON DELETE RESTRICT,
    description_ciphertext BYTEA NOT NULL,
    occurred_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT context_timeline_entries_description_length_check
        CHECK (octet_length(description_ciphertext) BETWEEN 1 AND 16384)
);

CREATE INDEX context_timeline_entries_summary_idx
    ON context_timeline_entries (context_summary_id, occurred_at, id);

CREATE TABLE context_timeline_sources (
    context_timeline_entry_id UUID NOT NULL
        REFERENCES context_timeline_entries (id) ON DELETE RESTRICT,
    chat_message_id UUID NOT NULL REFERENCES chat_messages (id) ON DELETE RESTRICT,
    PRIMARY KEY (context_timeline_entry_id, chat_message_id)
);

CREATE INDEX context_timeline_sources_message_idx
    ON context_timeline_sources (chat_message_id);

-- +goose Down

DROP TABLE IF EXISTS context_timeline_sources;
DROP TABLE IF EXISTS context_timeline_entries;

DROP INDEX IF EXISTS context_summaries_pending_review_idx;

ALTER TABLE context_items
    DROP CONSTRAINT IF EXISTS context_items_limitations_ciphertext_length_check,
    DROP CONSTRAINT IF EXISTS context_items_evidence_strength_check,
    DROP CONSTRAINT IF EXISTS context_items_impact_ciphertext_length_check,
    DROP CONSTRAINT IF EXISTS context_items_title_ciphertext_length_check,
    DROP CONSTRAINT IF EXISTS context_items_kind_check,
    DROP COLUMN IF EXISTS included,
    DROP COLUMN IF EXISTS limitations_ciphertext,
    DROP COLUMN IF EXISTS evidence_strength,
    DROP COLUMN IF EXISTS impact_ciphertext,
    DROP COLUMN IF EXISTS title_ciphertext,
    ADD CONSTRAINT context_items_kind_check
        CHECK (kind IN ('event', 'theme', 'marked_topic'));

ALTER TABLE context_summaries
    DROP CONSTRAINT IF EXISTS context_summaries_reviewed_at_check,
    DROP CONSTRAINT IF EXISTS context_summaries_review_status_check,
    DROP CONSTRAINT IF EXISTS context_summaries_graph_version_length_check,
    DROP CONSTRAINT IF EXISTS context_summaries_limitations_ciphertext_length_check,
    DROP CONSTRAINT IF EXISTS context_summaries_coverage_note_ciphertext_length_check,
    DROP CONSTRAINT IF EXISTS context_summaries_coverage_completeness_check,
    DROP CONSTRAINT IF EXISTS context_summaries_coverage_count_check,
    DROP CONSTRAINT IF EXISTS context_summaries_title_ciphertext_length_check,
    DROP CONSTRAINT IF EXISTS context_summaries_schema_version_length_check,
    DROP COLUMN IF EXISTS reviewed_at,
    DROP COLUMN IF EXISTS review_status,
    DROP COLUMN IF EXISTS graph_version,
    DROP COLUMN IF EXISTS limitations_ciphertext,
    DROP COLUMN IF EXISTS coverage_note_ciphertext,
    DROP COLUMN IF EXISTS coverage_completeness,
    DROP COLUMN IF EXISTS coverage_active_day_count,
    DROP COLUMN IF EXISTS coverage_user_message_count,
    DROP COLUMN IF EXISTS coverage_conversation_count,
    DROP COLUMN IF EXISTS title_ciphertext,
    DROP COLUMN IF EXISTS schema_version;
