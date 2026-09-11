-- +goose Up

ALTER TABLE professional_profiles
    DROP CONSTRAINT IF EXISTS professional_profiles_required_registration_check;

ALTER TABLE professional_profiles
    ADD CONSTRAINT professional_profiles_required_registration_check
    CHECK (
        registration_country_code IS NOT NULL
        AND char_length(btrim(registration_country_code)) = 2
        AND registration_region IS NOT NULL
        AND char_length(btrim(registration_region)) > 0
        AND (
            profession_type NOT IN (
                'psychologist',
                'psychiatrist',
                'occupational_therapist'
            )
            OR (
                registration_number IS NOT NULL
                AND char_length(btrim(registration_number)) > 0
            )
        )
    ) NOT VALID;

-- +goose Down

ALTER TABLE professional_profiles
    DROP CONSTRAINT IF EXISTS professional_profiles_required_registration_check;

ALTER TABLE professional_profiles
    ADD CONSTRAINT professional_profiles_required_registration_check
    CHECK (
        registration_country_code IS NOT NULL
        AND char_length(btrim(registration_country_code)) = 2
        AND registration_region IS NOT NULL
        AND char_length(btrim(registration_region)) > 0
        AND registration_number IS NOT NULL
        AND char_length(btrim(registration_number)) > 0
    ) NOT VALID;
