-- +goose Up

CREATE TABLE professional_webauthn_credentials (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    professional_user_id UUID NOT NULL
        REFERENCES professional_users (id) ON DELETE RESTRICT,
    credential_id BYTEA NOT NULL UNIQUE,
    credential_ciphertext BYTEA NOT NULL,
    label TEXT,
    created_ip INET,
    last_used_ip INET,
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT professional_webauthn_credentials_id_length_check
        CHECK (octet_length(credential_id) BETWEEN 1 AND 1024),
    CONSTRAINT professional_webauthn_credentials_ciphertext_length_check
        CHECK (octet_length(credential_ciphertext) BETWEEN 1 AND 65536),
    CONSTRAINT professional_webauthn_credentials_label_length_check
        CHECK (label IS NULL OR char_length(btrim(label)) BETWEEN 1 AND 100),
    CONSTRAINT professional_webauthn_credentials_last_used_at_check
        CHECK (last_used_at IS NULL OR last_used_at >= created_at),
    CONSTRAINT professional_webauthn_credentials_revoked_at_check
        CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);

CREATE INDEX professional_webauthn_credentials_professional_active_idx
    ON professional_webauthn_credentials (professional_user_id, created_at DESC)
    WHERE revoked_at IS NULL;

CREATE TABLE professional_webauthn_ceremonies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    professional_user_id UUID NOT NULL
        REFERENCES professional_users (id) ON DELETE RESTRICT,
    purpose TEXT NOT NULL,
    token_hash CHAR(64) NOT NULL UNIQUE,
    session_data_ciphertext BYTEA NOT NULL,
    requested_ip INET,
    user_agent TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT professional_webauthn_ceremonies_purpose_check
        CHECK (purpose IN ('registration', 'authentication')),
    CONSTRAINT professional_webauthn_ceremonies_token_hash_check
        CHECK (token_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT professional_webauthn_ceremonies_session_data_length_check
        CHECK (octet_length(session_data_ciphertext) BETWEEN 1 AND 65536),
    CONSTRAINT professional_webauthn_ceremonies_user_agent_length_check
        CHECK (user_agent IS NULL OR char_length(user_agent) <= 1024),
    CONSTRAINT professional_webauthn_ceremonies_expiration_check
        CHECK (expires_at > created_at),
    CONSTRAINT professional_webauthn_ceremonies_consumed_at_check
        CHECK (consumed_at IS NULL OR consumed_at >= created_at)
);

CREATE INDEX professional_webauthn_ceremonies_professional_purpose_idx
    ON professional_webauthn_ceremonies (
        professional_user_id,
        purpose,
        created_at DESC
    );

CREATE TABLE professional_recovery_codes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    professional_user_id UUID NOT NULL
        REFERENCES professional_users (id) ON DELETE RESTRICT,
    code_hash CHAR(64) NOT NULL UNIQUE,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT professional_recovery_codes_hash_check
        CHECK (code_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT professional_recovery_codes_used_at_check
        CHECK (used_at IS NULL OR used_at >= created_at)
);

CREATE INDEX professional_recovery_codes_professional_active_idx
    ON professional_recovery_codes (professional_user_id)
    WHERE used_at IS NULL;

INSERT INTO professional_recovery_codes (
    professional_user_id,
    code_hash,
    used_at,
    created_at
)
SELECT
    method.professional_user_id,
    recovery.code_hash,
    recovery.used_at,
    recovery.created_at
FROM professional_mfa_recovery_codes AS recovery
JOIN professional_mfa_methods AS method
    ON method.id = recovery.professional_mfa_method_id
ON CONFLICT (code_hash) DO NOTHING;

ALTER TABLE auth_events
    DROP CONSTRAINT auth_events_event_type_check;

ALTER TABLE auth_events
    ADD CONSTRAINT auth_events_event_type_check
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
                'passkey_registration_started',
                'passkey_registered',
                'passkey_authentication',
                'passkey_removed',
                'passkey_recovery_used',
                'passkey_recovery_codes_regenerated',
                'session_revoked'
            )
        );

-- +goose Down

ALTER TABLE auth_events
    DROP CONSTRAINT auth_events_event_type_check;

ALTER TABLE auth_events
    ADD CONSTRAINT auth_events_event_type_check
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
        );

DROP TABLE IF EXISTS professional_recovery_codes;
DROP TABLE IF EXISTS professional_webauthn_ceremonies;
DROP TABLE IF EXISTS professional_webauthn_credentials;
