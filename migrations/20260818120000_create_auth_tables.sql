-- +goose Up

CREATE TABLE auth_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    audience TEXT NOT NULL,
    app_user_id UUID REFERENCES app_users (id) ON DELETE RESTRICT,
    professional_user_id UUID REFERENCES professional_users (id) ON DELETE RESTRICT,
    token_family_id UUID NOT NULL DEFAULT gen_random_uuid(),
    refresh_token_hash CHAR(64) NOT NULL UNIQUE,
    replaced_by_session_id UUID REFERENCES auth_sessions (id) ON DELETE SET NULL,
    mfa_verified_at TIMESTAMPTZ,
    created_ip INET,
    last_used_ip INET,
    user_agent TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    last_used_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    rotated_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    revoke_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT auth_sessions_audience_check
        CHECK (audience IN ('app', 'professional')),
    CONSTRAINT auth_sessions_identity_check
        CHECK (
            (audience = 'app' AND app_user_id IS NOT NULL AND professional_user_id IS NULL)
            OR
            (audience = 'professional' AND professional_user_id IS NOT NULL AND app_user_id IS NULL)
        ),
    CONSTRAINT auth_sessions_refresh_token_hash_check
        CHECK (refresh_token_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT auth_sessions_user_agent_length_check
        CHECK (user_agent IS NULL OR char_length(user_agent) <= 1024),
    CONSTRAINT auth_sessions_expiration_check
        CHECK (expires_at > created_at),
    CONSTRAINT auth_sessions_rotated_at_check
        CHECK (rotated_at IS NULL OR rotated_at >= created_at),
    CONSTRAINT auth_sessions_revoked_at_check
        CHECK (revoked_at IS NULL OR revoked_at >= created_at),
    CONSTRAINT auth_sessions_revoke_reason_length_check
        CHECK (revoke_reason IS NULL OR char_length(btrim(revoke_reason)) BETWEEN 1 AND 100)
);

CREATE INDEX auth_sessions_app_user_active_idx
    ON auth_sessions (app_user_id, created_at DESC)
    WHERE revoked_at IS NULL;

CREATE INDEX auth_sessions_professional_user_active_idx
    ON auth_sessions (professional_user_id, created_at DESC)
    WHERE revoked_at IS NULL;

CREATE INDEX auth_sessions_token_family_idx
    ON auth_sessions (token_family_id);

CREATE TABLE auth_one_time_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    audience TEXT NOT NULL,
    app_user_id UUID REFERENCES app_users (id) ON DELETE RESTRICT,
    professional_user_id UUID REFERENCES professional_users (id) ON DELETE RESTRICT,
    purpose TEXT NOT NULL,
    token_hash CHAR(64) NOT NULL UNIQUE,
    requested_ip INET,
    user_agent TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT auth_one_time_tokens_audience_check
        CHECK (audience IN ('app', 'professional')),
    CONSTRAINT auth_one_time_tokens_identity_check
        CHECK (
            (audience = 'app' AND app_user_id IS NOT NULL AND professional_user_id IS NULL)
            OR
            (audience = 'professional' AND professional_user_id IS NOT NULL AND app_user_id IS NULL)
        ),
    CONSTRAINT auth_one_time_tokens_purpose_check
        CHECK (purpose IN ('email_verification', 'password_reset', 'mfa_login')),
    CONSTRAINT auth_one_time_tokens_token_hash_check
        CHECK (token_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT auth_one_time_tokens_user_agent_length_check
        CHECK (user_agent IS NULL OR char_length(user_agent) <= 1024),
    CONSTRAINT auth_one_time_tokens_expiration_check
        CHECK (expires_at > created_at),
    CONSTRAINT auth_one_time_tokens_consumed_at_check
        CHECK (consumed_at IS NULL OR consumed_at >= created_at)
);

CREATE INDEX auth_one_time_tokens_app_user_purpose_idx
    ON auth_one_time_tokens (app_user_id, purpose, created_at DESC);

CREATE INDEX auth_one_time_tokens_professional_user_purpose_idx
    ON auth_one_time_tokens (professional_user_id, purpose, created_at DESC);

CREATE TABLE professional_mfa_methods (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    professional_user_id UUID NOT NULL
        REFERENCES professional_users (id) ON DELETE RESTRICT,
    method TEXT NOT NULL,
    label TEXT,
    secret_ciphertext BYTEA NOT NULL,
    confirmed_at TIMESTAMPTZ,
    disabled_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT professional_mfa_methods_method_check
        CHECK (method = 'totp'),
    CONSTRAINT professional_mfa_methods_label_length_check
        CHECK (label IS NULL OR char_length(btrim(label)) BETWEEN 1 AND 100),
    CONSTRAINT professional_mfa_methods_secret_not_empty_check
        CHECK (octet_length(secret_ciphertext) > 0),
    CONSTRAINT professional_mfa_methods_confirmed_at_check
        CHECK (confirmed_at IS NULL OR confirmed_at >= created_at),
    CONSTRAINT professional_mfa_methods_disabled_at_check
        CHECK (disabled_at IS NULL OR disabled_at >= created_at)
);

CREATE UNIQUE INDEX professional_mfa_methods_active_totp_unique
    ON professional_mfa_methods (professional_user_id, method)
    WHERE disabled_at IS NULL;

CREATE TABLE professional_mfa_recovery_codes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    professional_mfa_method_id UUID NOT NULL
        REFERENCES professional_mfa_methods (id) ON DELETE RESTRICT,
    code_hash CHAR(64) NOT NULL UNIQUE,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT professional_mfa_recovery_codes_hash_check
        CHECK (code_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT professional_mfa_recovery_codes_used_at_check
        CHECK (used_at IS NULL OR used_at >= created_at)
);

CREATE INDEX professional_mfa_recovery_codes_method_idx
    ON professional_mfa_recovery_codes (professional_mfa_method_id)
    WHERE used_at IS NULL;

CREATE TABLE auth_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    audience TEXT NOT NULL,
    app_user_id UUID REFERENCES app_users (id) ON DELETE RESTRICT,
    professional_user_id UUID REFERENCES professional_users (id) ON DELETE RESTRICT,
    session_id UUID REFERENCES auth_sessions (id) ON DELETE SET NULL,
    event_type TEXT NOT NULL,
    outcome TEXT NOT NULL,
    ip_address INET,
    user_agent TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT auth_events_audience_check
        CHECK (audience IN ('app', 'professional')),
    CONSTRAINT auth_events_identity_check
        CHECK (
            (app_user_id IS NULL AND professional_user_id IS NULL)
            OR
            (audience = 'app' AND app_user_id IS NOT NULL AND professional_user_id IS NULL)
            OR
            (audience = 'professional' AND professional_user_id IS NOT NULL AND app_user_id IS NULL)
        ),
    CONSTRAINT auth_events_event_type_check
        CHECK (
            event_type IN (
                'register',
                'login',
                'logout',
                'logout_all',
                'refresh',
                'refresh_reuse',
                'email_verification_requested',
                'email_verified',
                'password_reset_requested',
                'password_reset_completed',
                'password_changed',
                'mfa_enrollment_started',
                'mfa_enabled',
                'mfa_challenge',
                'mfa_recovery_used',
                'mfa_recovery_codes_regenerated',
                'session_revoked'
            )
        ),
    CONSTRAINT auth_events_outcome_check
        CHECK (outcome IN ('success', 'failure')),
    CONSTRAINT auth_events_user_agent_length_check
        CHECK (user_agent IS NULL OR char_length(user_agent) <= 1024),
    CONSTRAINT auth_events_metadata_object_check
        CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX auth_events_app_user_created_idx
    ON auth_events (app_user_id, created_at DESC);

CREATE INDEX auth_events_professional_user_created_idx
    ON auth_events (professional_user_id, created_at DESC);

CREATE INDEX auth_events_created_idx
    ON auth_events (created_at DESC);

-- +goose Down

DROP TABLE IF EXISTS auth_events;
DROP TABLE IF EXISTS professional_mfa_recovery_codes;
DROP TABLE IF EXISTS professional_mfa_methods;
DROP TABLE IF EXISTS auth_one_time_tokens;
DROP TABLE IF EXISTS auth_sessions;
