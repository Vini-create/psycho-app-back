-- +goose Up

ALTER TABLE app_users
    DROP CONSTRAINT app_users_plan_check;

UPDATE app_users
SET plan = 'plus', updated_at = now()
WHERE plan = 'pro';

ALTER TABLE app_users
    ADD CONSTRAINT app_users_plan_check
        CHECK (plan IN ('free', 'plus'));

ALTER TABLE subscriptions
    DROP CONSTRAINT subscriptions_plan_check;

DO $$
DECLARE
    professional RECORD;
    new_organization_id UUID;
BEGIN
    FOR professional IN
        SELECT account.id, account.display_name
        FROM professional_users AS account
        WHERE account.deleted_at IS NULL
          AND NOT EXISTS (
              SELECT 1
              FROM organization_memberships AS membership
              WHERE membership.professional_user_id = account.id
                AND membership.status = 'active'
          )
    LOOP
        INSERT INTO organizations (name, kind)
        VALUES ('Consultório de ' || professional.display_name, 'solo')
        RETURNING id INTO new_organization_id;

        INSERT INTO organization_memberships (
            organization_id, professional_user_id, role, status, joined_at
        )
        VALUES (new_organization_id, professional.id, 'owner', 'active', now());
    END LOOP;
END $$;

INSERT INTO subscriptions (organization_id, provider, plan, status)
SELECT DISTINCT membership.organization_id, 'internal', 'pro', 'active'
FROM organization_memberships AS membership
JOIN professional_users AS professional
  ON professional.id = membership.professional_user_id
WHERE membership.status = 'active'
  AND professional.deleted_at IS NULL
ON CONFLICT (organization_id) DO UPDATE SET
    plan = 'pro',
    status = 'active',
    cancel_at_period_end = false,
    updated_at = now();

ALTER TABLE subscriptions
    ADD CONSTRAINT subscriptions_plan_check
        CHECK (plan IN ('free', 'plus', 'pro', 'consultorio', 'team', 'clinic'));

-- +goose Down

ALTER TABLE subscriptions
    DROP CONSTRAINT subscriptions_plan_check;

UPDATE subscriptions
SET plan = CASE WHEN plan = 'clinic' THEN 'clinic' ELSE 'single' END,
    updated_at = now();

ALTER TABLE subscriptions
    ADD CONSTRAINT subscriptions_plan_check
        CHECK (plan IN ('single', 'pro', 'clinic'));

ALTER TABLE app_users
    DROP CONSTRAINT app_users_plan_check;

UPDATE app_users
SET plan = 'pro', updated_at = now()
WHERE plan = 'plus';

ALTER TABLE app_users
    ADD CONSTRAINT app_users_plan_check
        CHECK (plan IN ('free', 'pro'));
