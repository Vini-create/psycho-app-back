-- +goose Up

CREATE TABLE professional_device_authorizations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    professional_user_id UUID NOT NULL
        REFERENCES professional_users (id) ON DELETE RESTRICT,
    webauthn_ceremony_id UUID NOT NULL UNIQUE
        REFERENCES professional_webauthn_ceremonies (id) ON DELETE CASCADE,
    scan_token_hash CHAR(64) NOT NULL UNIQUE,
    poll_token_hash CHAR(64) NOT NULL UNIQUE,
    confirmation_code CHAR(6) NOT NULL,
    public_key JSONB NOT NULL,
    approved_at TIMESTAMPTZ,
    consumed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT professional_device_authorizations_scan_hash_check
        CHECK (scan_token_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT professional_device_authorizations_poll_hash_check
        CHECK (poll_token_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT professional_device_authorizations_confirmation_code_check
        CHECK (confirmation_code ~ '^[0-9]{6}$'),
    CONSTRAINT professional_device_authorizations_public_key_check
        CHECK (jsonb_typeof(public_key) = 'object'),
    CONSTRAINT professional_device_authorizations_expiration_check
        CHECK (expires_at > created_at),
    CONSTRAINT professional_device_authorizations_approved_at_check
        CHECK (approved_at IS NULL OR approved_at >= created_at),
    CONSTRAINT professional_device_authorizations_consumed_at_check
        CHECK (consumed_at IS NULL OR (approved_at IS NOT NULL AND consumed_at >= approved_at))
);

CREATE INDEX professional_device_authorizations_pending_idx
    ON professional_device_authorizations (expires_at)
    WHERE consumed_at IS NULL;

-- +goose Down

DROP TABLE IF EXISTS professional_device_authorizations;
