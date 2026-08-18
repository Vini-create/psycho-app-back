-- +goose Up

-- +goose StatementBegin
CREATE FUNCTION app_text_array_items_have_valid_length(
    items TEXT[],
    minimum_length INTEGER,
    maximum_length INTEGER
)
RETURNS BOOLEAN
LANGUAGE SQL
IMMUTABLE
PARALLEL SAFE
AS $function$
    SELECT COALESCE(
        bool_and(
            item IS NOT NULL
            AND char_length(btrim(item)) BETWEEN minimum_length AND maximum_length
        ),
        true
    )
    FROM unnest(items) AS unnested(item);
$function$;
-- +goose StatementEnd

ALTER TABLE app_users
    ADD CONSTRAINT app_users_email_length_check
        CHECK (char_length(email) BETWEEN 3 AND 254),
    ADD CONSTRAINT app_users_password_hash_length_check
        CHECK (char_length(password_hash) BETWEEN 1 AND 512);

ALTER TABLE professional_users
    ADD CONSTRAINT professional_users_email_length_check
        CHECK (char_length(email) BETWEEN 3 AND 254),
    ADD CONSTRAINT professional_users_password_hash_length_check
        CHECK (char_length(password_hash) BETWEEN 1 AND 512);

ALTER TABLE professional_profiles
    ADD CONSTRAINT professional_profiles_country_code_format_check
        CHECK (
            registration_country_code IS NULL
            OR registration_country_code ~ '^[A-Z]{2}$'
        ),
    ADD CONSTRAINT professional_profiles_registration_region_length_check
        CHECK (
            registration_region IS NULL
            OR char_length(btrim(registration_region)) BETWEEN 1 AND 100
        ),
    ADD CONSTRAINT professional_profiles_registration_number_length_check
        CHECK (
            registration_number IS NULL
            OR char_length(btrim(registration_number)) BETWEEN 1 AND 100
        ),
    ADD CONSTRAINT professional_profiles_bio_content_length_check
        CHECK (
            bio IS NULL
            OR char_length(btrim(bio)) BETWEEN 1 AND 2000
        ),
    ADD CONSTRAINT professional_profiles_certifications_items_length_check
        CHECK (
            app_text_array_items_have_valid_length(certifications, 1, 200)
        );

ALTER TABLE subscriptions
    ADD CONSTRAINT subscriptions_provider_length_check
        CHECK (char_length(btrim(provider)) BETWEEN 1 AND 32),
    ADD CONSTRAINT subscriptions_provider_customer_id_length_check
        CHECK (
            provider_customer_id IS NULL
            OR char_length(provider_customer_id) BETWEEN 1 AND 255
        ),
    ADD CONSTRAINT subscriptions_provider_subscription_id_length_check
        CHECK (
            provider_subscription_id IS NULL
            OR char_length(provider_subscription_id) BETWEEN 1 AND 255
        );

ALTER TABLE connection_invitations
    ADD CONSTRAINT connection_invitations_target_email_length_check
        CHECK (char_length(target_email) BETWEEN 3 AND 254),
    ADD CONSTRAINT connection_invitations_token_hash_length_check
        CHECK (char_length(token_hash) = 64);

ALTER TABLE connection_consents
    ADD CONSTRAINT connection_consents_policy_version_length_check
        CHECK (char_length(btrim(policy_version)) BETWEEN 1 AND 64),
    ADD CONSTRAINT connection_consents_user_agent_length_check
        CHECK (
            user_agent IS NULL
            OR char_length(user_agent) <= 1024
        );

-- +goose Down

ALTER TABLE connection_consents
    DROP CONSTRAINT IF EXISTS connection_consents_user_agent_length_check,
    DROP CONSTRAINT IF EXISTS connection_consents_policy_version_length_check;

ALTER TABLE connection_invitations
    DROP CONSTRAINT IF EXISTS connection_invitations_token_hash_length_check,
    DROP CONSTRAINT IF EXISTS connection_invitations_target_email_length_check;

ALTER TABLE subscriptions
    DROP CONSTRAINT IF EXISTS subscriptions_provider_subscription_id_length_check,
    DROP CONSTRAINT IF EXISTS subscriptions_provider_customer_id_length_check,
    DROP CONSTRAINT IF EXISTS subscriptions_provider_length_check;

ALTER TABLE professional_profiles
    DROP CONSTRAINT IF EXISTS professional_profiles_certifications_items_length_check,
    DROP CONSTRAINT IF EXISTS professional_profiles_bio_content_length_check,
    DROP CONSTRAINT IF EXISTS professional_profiles_registration_number_length_check,
    DROP CONSTRAINT IF EXISTS professional_profiles_registration_region_length_check,
    DROP CONSTRAINT IF EXISTS professional_profiles_country_code_format_check;

ALTER TABLE professional_users
    DROP CONSTRAINT IF EXISTS professional_users_password_hash_length_check,
    DROP CONSTRAINT IF EXISTS professional_users_email_length_check;

ALTER TABLE app_users
    DROP CONSTRAINT IF EXISTS app_users_password_hash_length_check,
    DROP CONSTRAINT IF EXISTS app_users_email_length_check;

DROP FUNCTION IF EXISTS app_text_array_items_have_valid_length(TEXT[], INTEGER, INTEGER);
