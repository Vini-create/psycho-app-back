-- +goose Up

CREATE TABLE context_report_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id UUID NOT NULL
        REFERENCES professional_patient_connections (id) ON DELETE RESTRICT,
    requested_by_professional_user_id UUID NOT NULL
        REFERENCES professional_users (id) ON DELETE RESTRICT,
    processing_job_id UUID UNIQUE
        REFERENCES context_processing_jobs (id) ON DELETE RESTRICT,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT context_report_requests_period_check
        CHECK (period_end > period_start),
    CONSTRAINT context_report_requests_period_length_check
        CHECK (period_end - period_start <= interval '31 days'),
    CONSTRAINT context_report_requests_status_check
        CHECK (status IN ('pending', 'processing', 'sent', 'declined', 'expired', 'failed')),
    CONSTRAINT context_report_requests_job_check
        CHECK (
            (status IN ('processing', 'sent', 'failed')) = (processing_job_id IS NOT NULL)
        ),
    CONSTRAINT context_report_requests_sent_at_check
        CHECK ((status = 'sent') = (sent_at IS NOT NULL))
);

CREATE UNIQUE INDEX context_report_requests_open_period_unique
    ON context_report_requests (connection_id, period_start, period_end)
    WHERE status IN ('pending', 'processing');

CREATE INDEX context_report_requests_connection_recent_idx
    ON context_report_requests (connection_id, requested_at DESC);

CREATE INDEX context_report_requests_patient_pending_idx
    ON context_report_requests (connection_id, requested_at DESC)
    WHERE status = 'pending';

-- A unicidade passa a ser garantida pela solicitação aberta. Jobs antigos sem
-- autorização permanecem inertes e não bloqueiam uma solicitação nova.
DROP INDEX context_processing_jobs_open_period_unique;

-- +goose Down

DROP TABLE IF EXISTS context_report_requests;

CREATE UNIQUE INDEX context_processing_jobs_open_period_unique
    ON context_processing_jobs (connection_id, period_start, period_end)
    WHERE status IN ('queued', 'processing');
