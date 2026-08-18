-- +goose Up

CREATE TABLE app_users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    display_name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending_verification',
    plan TEXT NOT NULL DEFAULT 'free',
    email_verified_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT app_users_email_normalized_check
        CHECK (email = lower(btrim(email))),
    CONSTRAINT app_users_password_hash_not_empty_check
        CHECK (char_length(password_hash) > 0),
    CONSTRAINT app_users_display_name_length_check
        CHECK (char_length(btrim(display_name)) BETWEEN 1 AND 120),
    CONSTRAINT app_users_status_check
        CHECK (status IN ('pending_verification', 'active', 'suspended', 'deleted')),
    CONSTRAINT app_users_plan_check
        CHECK (plan IN ('free', 'pro'))
);

CREATE UNIQUE INDEX app_users_email_unique
    ON app_users (lower(email));

CREATE TABLE professional_users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    display_name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending_verification',
    email_verified_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT professional_users_email_normalized_check
        CHECK (email = lower(btrim(email))),
    CONSTRAINT professional_users_password_hash_not_empty_check
        CHECK (char_length(password_hash) > 0),
    CONSTRAINT professional_users_display_name_length_check
        CHECK (char_length(btrim(display_name)) BETWEEN 1 AND 120),
    CONSTRAINT professional_users_status_check
        CHECK (status IN ('pending_verification', 'active', 'suspended', 'deleted'))
);

CREATE UNIQUE INDEX professional_users_email_unique
    ON professional_users (lower(email));

CREATE TABLE professional_profiles (
    professional_user_id UUID PRIMARY KEY
        REFERENCES professional_users (id) ON DELETE CASCADE,
    profession_type TEXT NOT NULL,
    registration_country_code CHAR(2),
    registration_region TEXT,
    registration_number TEXT,
    bio TEXT,
    certifications TEXT[] NOT NULL DEFAULT '{}',
    verification_status TEXT NOT NULL DEFAULT 'unverified',
    verified_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT professional_profiles_profession_type_check
        CHECK (
            profession_type IN (
                'psychologist',
                'psychiatrist',
                'psychoanalyst',
                'therapist',
                'psychotherapist',
                'occupational_therapist',
                'counselor',
                'other'
            )
        ),
    CONSTRAINT professional_profiles_bio_length_check
        CHECK (bio IS NULL OR char_length(bio) <= 2000),
    CONSTRAINT professional_profiles_certifications_count_check
        CHECK (cardinality(certifications) <= 50),
    CONSTRAINT professional_profiles_country_code_check
        CHECK (
            registration_country_code IS NULL
            OR registration_country_code = upper(registration_country_code)
        ),
    CONSTRAINT professional_profiles_verification_status_check
        CHECK (verification_status IN ('unverified', 'pending', 'verified', 'rejected')),
    CONSTRAINT professional_profiles_verified_at_check
        CHECK (verification_status <> 'verified' OR verified_at IS NOT NULL)
);

CREATE UNIQUE INDEX professional_profiles_registration_unique
    ON professional_profiles (
        profession_type,
        registration_country_code,
        registration_region,
        registration_number
    )
    WHERE registration_number IS NOT NULL;

CREATE TABLE organizations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT organizations_name_length_check
        CHECK (char_length(btrim(name)) BETWEEN 1 AND 160),
    CONSTRAINT organizations_kind_check
        CHECK (kind IN ('solo', 'clinic')),
    CONSTRAINT organizations_status_check
        CHECK (status IN ('active', 'suspended', 'closed'))
);

CREATE TABLE organization_memberships (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL
        REFERENCES organizations (id) ON DELETE RESTRICT,
    professional_user_id UUID NOT NULL
        REFERENCES professional_users (id) ON DELETE RESTRICT,
    role TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    joined_at TIMESTAMPTZ,
    ended_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT organization_memberships_role_check
        CHECK (role IN ('owner', 'admin', 'professional')),
    CONSTRAINT organization_memberships_status_check
        CHECK (status IN ('invited', 'active', 'removed')),
    CONSTRAINT organization_memberships_joined_at_check
        CHECK (status <> 'active' OR joined_at IS NOT NULL),
    CONSTRAINT organization_memberships_ended_at_check
        CHECK (ended_at IS NULL OR joined_at IS NULL OR ended_at >= joined_at),
    CONSTRAINT organization_memberships_member_unique
        UNIQUE (organization_id, professional_user_id),
    CONSTRAINT organization_memberships_id_organization_unique
        UNIQUE (id, organization_id)
);

CREATE INDEX organization_memberships_professional_user_idx
    ON organization_memberships (professional_user_id);

CREATE TABLE subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL UNIQUE
        REFERENCES organizations (id) ON DELETE RESTRICT,
    provider TEXT NOT NULL,
    provider_customer_id TEXT,
    provider_subscription_id TEXT,
    plan TEXT NOT NULL,
    status TEXT NOT NULL,
    current_period_start TIMESTAMPTZ,
    current_period_end TIMESTAMPTZ,
    cancel_at_period_end BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT subscriptions_provider_not_empty_check
        CHECK (char_length(btrim(provider)) > 0),
    CONSTRAINT subscriptions_plan_check
        CHECK (plan IN ('single', 'pro', 'clinic')),
    CONSTRAINT subscriptions_status_check
        CHECK (status IN ('trialing', 'active', 'past_due', 'canceled', 'unpaid', 'inactive')),
    CONSTRAINT subscriptions_period_check
        CHECK (
            current_period_end IS NULL
            OR current_period_start IS NULL
            OR current_period_end >= current_period_start
        )
);

CREATE UNIQUE INDEX subscriptions_provider_customer_unique
    ON subscriptions (provider, provider_customer_id)
    WHERE provider_customer_id IS NOT NULL;

CREATE UNIQUE INDEX subscriptions_provider_subscription_unique
    ON subscriptions (provider, provider_subscription_id)
    WHERE provider_subscription_id IS NOT NULL;

CREATE TABLE connection_invitations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    professional_membership_id UUID NOT NULL,
    target_email TEXT NOT NULL,
    target_app_user_id UUID
        REFERENCES app_users (id) ON DELETE RESTRICT,
    token_hash TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'pending',
    expires_at TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT connection_invitations_membership_fk
        FOREIGN KEY (professional_membership_id, organization_id)
        REFERENCES organization_memberships (id, organization_id)
        ON DELETE RESTRICT,
    CONSTRAINT connection_invitations_email_normalized_check
        CHECK (target_email = lower(btrim(target_email))),
    CONSTRAINT connection_invitations_token_hash_not_empty_check
        CHECK (char_length(token_hash) > 0),
    CONSTRAINT connection_invitations_status_check
        CHECK (status IN ('pending', 'accepted', 'expired', 'revoked')),
    CONSTRAINT connection_invitations_expiration_check
        CHECK (expires_at > created_at),
    CONSTRAINT connection_invitations_accepted_at_check
        CHECK (status <> 'accepted' OR accepted_at IS NOT NULL),
    CONSTRAINT connection_invitations_revoked_at_check
        CHECK (status <> 'revoked' OR revoked_at IS NOT NULL)
);

CREATE INDEX connection_invitations_target_email_idx
    ON connection_invitations (lower(target_email));

CREATE INDEX connection_invitations_professional_membership_idx
    ON connection_invitations (professional_membership_id);

CREATE TABLE professional_patient_connections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    professional_membership_id UUID NOT NULL,
    app_user_id UUID NOT NULL
        REFERENCES app_users (id) ON DELETE RESTRICT,
    source_invitation_id UUID UNIQUE
        REFERENCES connection_invitations (id) ON DELETE RESTRICT,
    requested_by TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    professional_accepted_at TIMESTAMPTZ,
    app_user_accepted_at TIMESTAMPTZ,
    activated_at TIMESTAMPTZ,
    ended_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT professional_patient_connections_membership_fk
        FOREIGN KEY (professional_membership_id, organization_id)
        REFERENCES organization_memberships (id, organization_id)
        ON DELETE RESTRICT,
    CONSTRAINT professional_patient_connections_requested_by_check
        CHECK (requested_by IN ('professional', 'app_user')),
    CONSTRAINT professional_patient_connections_status_check
        CHECK (status IN ('pending', 'active', 'rejected', 'ended', 'revoked')),
    CONSTRAINT professional_patient_connections_active_check
        CHECK (
            status <> 'active'
            OR (
                professional_accepted_at IS NOT NULL
                AND app_user_accepted_at IS NOT NULL
                AND activated_at IS NOT NULL
            )
        ),
    CONSTRAINT professional_patient_connections_ended_at_check
        CHECK (ended_at IS NULL OR ended_at >= created_at)
);

CREATE UNIQUE INDEX professional_patient_connections_open_unique
    ON professional_patient_connections (
        organization_id,
        professional_membership_id,
        app_user_id
    )
    WHERE status IN ('pending', 'active');

CREATE INDEX professional_patient_connections_app_user_idx
    ON professional_patient_connections (app_user_id, status);

CREATE INDEX professional_patient_connections_professional_idx
    ON professional_patient_connections (professional_membership_id, status);

CREATE TABLE connection_consents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id UUID NOT NULL
        REFERENCES professional_patient_connections (id) ON DELETE RESTRICT,
    scope TEXT NOT NULL,
    policy_version TEXT NOT NULL,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ,
    ip_address INET,
    user_agent TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT connection_consents_scope_check
        CHECK (scope IN ('summaries', 'events', 'marked_topics', 'raw_messages')),
    CONSTRAINT connection_consents_policy_version_not_empty_check
        CHECK (char_length(btrim(policy_version)) > 0),
    CONSTRAINT connection_consents_revoked_at_check
        CHECK (revoked_at IS NULL OR revoked_at >= granted_at)
);

CREATE UNIQUE INDEX connection_consents_active_scope_unique
    ON connection_consents (connection_id, scope)
    WHERE revoked_at IS NULL;

CREATE INDEX connection_consents_connection_idx
    ON connection_consents (connection_id);

-- +goose Down

DROP TABLE IF EXISTS connection_consents;
DROP TABLE IF EXISTS professional_patient_connections;
DROP TABLE IF EXISTS connection_invitations;
DROP TABLE IF EXISTS subscriptions;
DROP TABLE IF EXISTS organization_memberships;
DROP TABLE IF EXISTS organizations;
DROP TABLE IF EXISTS professional_profiles;
DROP TABLE IF EXISTS professional_users;
DROP TABLE IF EXISTS app_users;
