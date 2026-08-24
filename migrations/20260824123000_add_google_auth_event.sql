-- +goose Up

ALTER TABLE auth_events
    DROP CONSTRAINT auth_events_event_type_check;

ALTER TABLE auth_events
    ADD CONSTRAINT auth_events_event_type_check
        CHECK (
            event_type IN (
                'register',
                'login',
                'google_login',
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
