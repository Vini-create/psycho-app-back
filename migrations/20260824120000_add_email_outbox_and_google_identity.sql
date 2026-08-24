-- +goose Up

CREATE TABLE auth_external_identities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    audience TEXT NOT NULL,
    app_user_id UUID REFERENCES app_users (id) ON DELETE CASCADE,
    professional_user_id UUID REFERENCES professional_users (id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    subject TEXT NOT NULL,
    provider_email TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT auth_external_identities_audience_check
        CHECK (audience IN ('app', 'professional')),
    CONSTRAINT auth_external_identities_identity_check
        CHECK (
            (audience = 'app' AND app_user_id IS NOT NULL AND professional_user_id IS NULL)
            OR
            (audience = 'professional' AND professional_user_id IS NOT NULL AND app_user_id IS NULL)
        ),
    CONSTRAINT auth_external_identities_provider_check
        CHECK (provider = 'google'),
    CONSTRAINT auth_external_identities_subject_length_check
        CHECK (char_length(subject) BETWEEN 1 AND 255),
    CONSTRAINT auth_external_identities_email_normalized_check
        CHECK (provider_email = lower(btrim(provider_email))),
    CONSTRAINT auth_external_identities_email_length_check
        CHECK (char_length(provider_email) BETWEEN 3 AND 254),
    UNIQUE (audience, provider, subject)
);

CREATE UNIQUE INDEX auth_external_identities_app_provider_unique
    ON auth_external_identities (app_user_id, provider)
    WHERE app_user_id IS NOT NULL;

CREATE UNIQUE INDEX auth_external_identities_professional_provider_unique
    ON auth_external_identities (professional_user_id, provider)
    WHERE professional_user_id IS NOT NULL;

CREATE TABLE auth_google_challenges (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    audience TEXT NOT NULL,
    nonce_hash CHAR(64) NOT NULL UNIQUE,
    requested_ip INET,
    user_agent TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT auth_google_challenges_audience_check
        CHECK (audience IN ('app', 'professional')),
    CONSTRAINT auth_google_challenges_nonce_hash_check
        CHECK (nonce_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT auth_google_challenges_user_agent_length_check
        CHECK (user_agent IS NULL OR char_length(user_agent) <= 1024),
    CONSTRAINT auth_google_challenges_expiration_check
        CHECK (expires_at > created_at),
    CONSTRAINT auth_google_challenges_consumed_at_check
        CHECK (consumed_at IS NULL OR consumed_at >= created_at)
);

CREATE INDEX auth_google_challenges_expiry_idx
    ON auth_google_challenges (expires_at)
    WHERE consumed_at IS NULL;

CREATE TABLE auth_email_outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    audience TEXT NOT NULL,
    kind TEXT NOT NULL,
    recipient_email TEXT NOT NULL,
    token_ciphertext BYTEA NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_expires_at TIMESTAMPTZ,
    provider_message_id TEXT,
    last_error_code TEXT,
    sent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT auth_email_outbox_audience_check
        CHECK (audience IN ('app', 'professional')),
    CONSTRAINT auth_email_outbox_kind_check
        CHECK (kind IN ('email_verification', 'password_reset')),
    CONSTRAINT auth_email_outbox_email_normalized_check
        CHECK (recipient_email = lower(btrim(recipient_email))),
    CONSTRAINT auth_email_outbox_email_length_check
        CHECK (char_length(recipient_email) BETWEEN 3 AND 254),
    CONSTRAINT auth_email_outbox_ciphertext_check
        CHECK (octet_length(token_ciphertext) > 0),
    CONSTRAINT auth_email_outbox_status_check
        CHECK (status IN ('queued', 'processing', 'sent', 'failed')),
    CONSTRAINT auth_email_outbox_attempt_count_check
        CHECK (attempt_count >= 0),
    CONSTRAINT auth_email_outbox_provider_message_id_length_check
        CHECK (provider_message_id IS NULL OR char_length(provider_message_id) <= 512),
    CONSTRAINT auth_email_outbox_last_error_code_length_check
        CHECK (last_error_code IS NULL OR char_length(last_error_code) <= 100),
    CONSTRAINT auth_email_outbox_sent_at_check
        CHECK (sent_at IS NULL OR sent_at >= created_at)
);

CREATE INDEX auth_email_outbox_claim_idx
    ON auth_email_outbox (available_at, created_at)
    WHERE status IN ('queued', 'processing');

-- +goose Down

DROP TABLE IF EXISTS auth_email_outbox;
DROP TABLE IF EXISTS auth_google_challenges;
DROP TABLE IF EXISTS auth_external_identities;
