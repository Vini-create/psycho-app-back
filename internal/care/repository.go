package care

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) UpsertProfessionalProfile(
	ctx context.Context,
	professionalUserID string,
	input ProfessionalProfileInput,
	now time.Time,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin professional onboarding: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var displayName string
	if err := tx.QueryRow(ctx, `
		SELECT display_name
		FROM professional_users
		WHERE id = $1 AND status = 'active' AND deleted_at IS NULL
		FOR UPDATE
	`, professionalUserID).Scan(&displayName); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("lock professional account: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO professional_profiles (
			professional_user_id, profession_type, registration_country_code,
			registration_region, registration_number, bio, certifications, updated_at
		)
		VALUES (
			$1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''),
			NULLIF($6, ''), $7, $8
		)
		ON CONFLICT (professional_user_id) DO UPDATE SET
			profession_type = EXCLUDED.profession_type,
			registration_country_code = EXCLUDED.registration_country_code,
			registration_region = EXCLUDED.registration_region,
			registration_number = EXCLUDED.registration_number,
			bio = EXCLUDED.bio,
			certifications = EXCLUDED.certifications,
			updated_at = EXCLUDED.updated_at
	`,
		professionalUserID,
		input.ProfessionType,
		input.RegistrationCountryCode,
		input.RegistrationRegion,
		input.RegistrationNumber,
		input.Bio,
		input.Certifications,
		now,
	); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrConflict
		}
		return fmt.Errorf("upsert professional profile: %w", err)
	}

	var activeMembershipExists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM organization_memberships
			WHERE professional_user_id = $1 AND status = 'active'
		)
	`, professionalUserID).Scan(&activeMembershipExists); err != nil {
		return fmt.Errorf("check professional workspace: %w", err)
	}

	if !activeMembershipExists {
		var organizationID string
		organizationName := "Consultório de " + displayName
		if err := tx.QueryRow(ctx, `
			INSERT INTO organizations (name, kind)
			VALUES ($1, 'solo')
			RETURNING id::text
		`, organizationName).Scan(&organizationID); err != nil {
			return fmt.Errorf("create solo organization: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO organization_memberships (
				organization_id, professional_user_id, role, status, joined_at
			)
			VALUES ($1, $2, 'owner', 'active', $3)
		`, organizationID, professionalUserID, now); err != nil {
			return fmt.Errorf("create organization owner membership: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO subscriptions (organization_id, provider, plan, status)
			VALUES ($1, 'internal', 'single', 'trialing')
		`, organizationID); err != nil {
			return fmt.Errorf("create trial subscription: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit professional onboarding: %w", err)
	}
	return nil
}

func (r *Repository) GetProfessionalProfile(
	ctx context.Context,
	professionalUserID string,
) (ProfessionalProfile, error) {
	var profile ProfessionalProfile
	err := r.pool.QueryRow(ctx, `
		SELECT professional.id::text, professional.display_name, professional.email,
		       profile.profession_type, profile.registration_country_code,
		       profile.registration_region, profile.registration_number, profile.bio,
		       profile.certifications, profile.verification_status,
		       organization.id::text, organization.name, membership.id::text,
		       COALESCE(subscription.plan, 'single')
		FROM professional_users AS professional
		JOIN professional_profiles AS profile
		  ON profile.professional_user_id = professional.id
		JOIN organization_memberships AS membership
		  ON membership.professional_user_id = professional.id AND membership.status = 'active'
		JOIN organizations AS organization
		  ON organization.id = membership.organization_id AND organization.status = 'active'
		LEFT JOIN subscriptions AS subscription ON subscription.organization_id = organization.id
		WHERE professional.id = $1 AND professional.deleted_at IS NULL
		ORDER BY membership.created_at
		LIMIT 1
	`, professionalUserID).Scan(
		&profile.ProfessionalUserID,
		&profile.DisplayName,
		&profile.Email,
		&profile.ProfessionType,
		&profile.RegistrationCountryCode,
		&profile.RegistrationRegion,
		&profile.RegistrationNumber,
		&profile.Bio,
		&profile.Certifications,
		&profile.VerificationStatus,
		&profile.OrganizationID,
		&profile.OrganizationName,
		&profile.MembershipID,
		&profile.Plan,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProfessionalProfile{}, ErrNotFound
	}
	if err != nil {
		return ProfessionalProfile{}, fmt.Errorf("get professional profile: %w", err)
	}
	profile.OnboardingComplete = profile.RegistrationCountryCode != nil &&
		len(strings.TrimSpace(*profile.RegistrationCountryCode)) == 2 &&
		profile.RegistrationRegion != nil && strings.TrimSpace(*profile.RegistrationRegion) != "" &&
		profile.RegistrationNumber != nil && strings.TrimSpace(*profile.RegistrationNumber) != ""
	return profile, nil
}

func (r *Repository) CreateInvitation(
	ctx context.Context,
	professionalUserID string,
	targetEmail string,
	tokenHash string,
	expiresAt time.Time,
	now time.Time,
) (Invitation, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Invitation{}, fmt.Errorf("begin invitation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var membershipID string
	var organizationID string
	if err := tx.QueryRow(ctx, `
		SELECT membership.id::text, membership.organization_id::text
		FROM organization_memberships AS membership
		JOIN organizations AS organization ON organization.id = membership.organization_id
		WHERE membership.professional_user_id = $1
		  AND membership.status = 'active'
		  AND organization.status = 'active'
		ORDER BY membership.created_at
		LIMIT 1
		FOR UPDATE OF membership
	`, professionalUserID).Scan(&membershipID, &organizationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Invitation{}, ErrForbidden
		}
		return Invitation{}, fmt.Errorf("select invitation membership: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE connection_invitations
		SET status = 'revoked', revoked_at = $4, updated_at = $4
		WHERE organization_id = $1
		  AND professional_membership_id = $2
		  AND target_email = $3
		  AND status = 'pending'
	`, organizationID, membershipID, targetEmail, now); err != nil {
		return Invitation{}, fmt.Errorf("revoke previous invitation: %w", err)
	}

	var targetAppUserID *string
	if err := tx.QueryRow(ctx, `
		SELECT id::text FROM app_users WHERE email = $1 AND deleted_at IS NULL
	`, targetEmail).Scan(&targetAppUserID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, fmt.Errorf("find invited app user: %w", err)
	}

	var invitation Invitation
	err = tx.QueryRow(ctx, `
		INSERT INTO connection_invitations (
			organization_id, professional_membership_id, target_email,
			target_app_user_id, token_hash, expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id::text, target_email, status, expires_at,
		          accepted_at, revoked_at, created_at
	`, organizationID, membershipID, targetEmail, targetAppUserID, tokenHash, expiresAt).Scan(
		&invitation.ID,
		&invitation.TargetEmail,
		&invitation.Status,
		&invitation.ExpiresAt,
		&invitation.AcceptedAt,
		&invitation.RevokedAt,
		&invitation.CreatedAt,
	)
	if err != nil {
		return Invitation{}, fmt.Errorf("insert connection invitation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Invitation{}, fmt.Errorf("commit connection invitation: %w", err)
	}
	return invitation, nil
}

func (r *Repository) ListInvitations(
	ctx context.Context,
	professionalUserID string,
	now time.Time,
) ([]Invitation, error) {
	if _, err := r.pool.Exec(ctx, `
		UPDATE connection_invitations AS invitation
		SET status = 'expired', updated_at = $2
		FROM organization_memberships AS membership
		WHERE invitation.professional_membership_id = membership.id
		  AND membership.professional_user_id = $1
		  AND invitation.status = 'pending'
		  AND invitation.expires_at <= $2
	`, professionalUserID, now); err != nil {
		return nil, fmt.Errorf("expire professional invitations: %w", err)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT invitation.id::text, invitation.target_email, invitation.status,
		       invitation.expires_at, invitation.accepted_at, invitation.revoked_at,
		       invitation.created_at
		FROM connection_invitations AS invitation
		JOIN organization_memberships AS membership
		  ON membership.id = invitation.professional_membership_id
		WHERE membership.professional_user_id = $1
		ORDER BY invitation.created_at DESC
		LIMIT 100
	`, professionalUserID)
	if err != nil {
		return nil, fmt.Errorf("query professional invitations: %w", err)
	}
	defer rows.Close()

	invitations := make([]Invitation, 0)
	for rows.Next() {
		var invitation Invitation
		if err := rows.Scan(
			&invitation.ID,
			&invitation.TargetEmail,
			&invitation.Status,
			&invitation.ExpiresAt,
			&invitation.AcceptedAt,
			&invitation.RevokedAt,
			&invitation.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan professional invitation: %w", err)
		}
		invitations = append(invitations, invitation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate professional invitations: %w", err)
	}
	return invitations, nil
}

func (r *Repository) RevokeInvitation(
	ctx context.Context,
	professionalUserID string,
	invitationID string,
	now time.Time,
) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE connection_invitations AS invitation
		SET status = 'revoked', revoked_at = $3, updated_at = $3
		FROM organization_memberships AS membership
		WHERE invitation.id = $1
		  AND invitation.professional_membership_id = membership.id
		  AND membership.professional_user_id = $2
		  AND invitation.status = 'pending'
	`, invitationID, professionalUserID, now)
	if err != nil {
		return fmt.Errorf("revoke connection invitation: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) PreviewInvitation(
	ctx context.Context,
	tokenHash string,
	now time.Time,
) (InvitationPreview, error) {
	var preview InvitationPreview
	var targetEmail string
	err := r.pool.QueryRow(ctx, `
		SELECT professional.display_name, profile.profession_type,
		       organization.name, invitation.target_email, invitation.expires_at
		FROM connection_invitations AS invitation
		JOIN organization_memberships AS membership
		  ON membership.id = invitation.professional_membership_id
		JOIN professional_users AS professional
		  ON professional.id = membership.professional_user_id
		JOIN professional_profiles AS profile
		  ON profile.professional_user_id = professional.id
		JOIN organizations AS organization ON organization.id = invitation.organization_id
		WHERE invitation.token_hash = $1
		  AND invitation.status = 'pending'
		  AND invitation.expires_at > $2
		  AND membership.status = 'active'
		  AND professional.status = 'active'
		  AND organization.status = 'active'
	`, tokenHash, now).Scan(
		&preview.ProfessionalDisplayName,
		&preview.ProfessionType,
		&preview.OrganizationName,
		&targetEmail,
		&preview.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return InvitationPreview{}, ErrInvalidToken
	}
	if err != nil {
		return InvitationPreview{}, fmt.Errorf("preview connection invitation: %w", err)
	}
	preview.TargetEmailHint = maskEmail(targetEmail)
	return preview, nil
}

func (r *Repository) AcceptInvitation(
	ctx context.Context,
	appUserID string,
	tokenHash string,
	consentScopes []string,
	policyVersion string,
	client ClientInfo,
	now time.Time,
) (string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin invitation acceptance: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var appEmail string
	if err := tx.QueryRow(ctx, `
		SELECT email FROM app_users
		WHERE id = $1 AND status = 'active' AND deleted_at IS NULL
	`, appUserID).Scan(&appEmail); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrForbidden
		}
		return "", fmt.Errorf("find invitation app user: %w", err)
	}

	var invitationID string
	var organizationID string
	var membershipID string
	var targetEmail string
	err = tx.QueryRow(ctx, `
		SELECT invitation.id::text, invitation.organization_id::text,
		       invitation.professional_membership_id::text, invitation.target_email
		FROM connection_invitations AS invitation
		JOIN organization_memberships AS membership
		  ON membership.id = invitation.professional_membership_id
		WHERE invitation.token_hash = $1
		  AND invitation.status = 'pending'
		  AND invitation.expires_at > $2
		  AND membership.status = 'active'
		FOR UPDATE OF invitation
	`, tokenHash, now).Scan(
		&invitationID, &organizationID, &membershipID, &targetEmail,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrInvalidToken
	}
	if err != nil {
		return "", fmt.Errorf("lock connection invitation: %w", err)
	}
	if appEmail != targetEmail {
		return "", ErrForbidden
	}

	if _, err := tx.Exec(ctx, `
		UPDATE connection_invitations
		SET target_app_user_id = $2, status = 'accepted', accepted_at = $3, updated_at = $3
		WHERE id = $1
	`, invitationID, appUserID, now); err != nil {
		return "", fmt.Errorf("accept connection invitation: %w", err)
	}

	var connectionID string
	err = tx.QueryRow(ctx, `
		INSERT INTO professional_patient_connections (
			organization_id, professional_membership_id, app_user_id,
			source_invitation_id, requested_by, status,
			professional_accepted_at, app_user_accepted_at, activated_at
		)
		VALUES ($1, $2, $3, $4, 'professional', 'active', $5, $5, $5)
		RETURNING id::text
	`, organizationID, membershipID, appUserID, invitationID, now).Scan(&connectionID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return "", ErrConflict
		}
		return "", fmt.Errorf("create professional patient connection: %w", err)
	}

	for _, scope := range consentScopes {
		if _, err := tx.Exec(ctx, `
			INSERT INTO connection_consents (
				connection_id, scope, policy_version, granted_at, ip_address, user_agent
			)
			VALUES ($1, $2, $3, $4, NULLIF($5, '')::inet, NULLIF($6, ''))
		`, connectionID, scope, policyVersion, now, client.IPAddress, client.UserAgent); err != nil {
			return "", fmt.Errorf("grant connection consent: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit invitation acceptance: %w", err)
	}
	return connectionID, nil
}

func (r *Repository) ListAppConnections(
	ctx context.Context,
	appUserID string,
) ([]Connection, error) {
	return r.listConnections(ctx, `connection.app_user_id = $1`, appUserID)
}

func (r *Repository) ListProfessionalPatients(
	ctx context.Context,
	professionalUserID string,
) ([]Connection, error) {
	return r.listConnections(ctx, `membership.professional_user_id = $1`, professionalUserID)
}

func (r *Repository) GetProfessionalPatient(
	ctx context.Context,
	professionalUserID string,
	connectionID string,
) (Connection, error) {
	connections, err := r.listConnections(
		ctx,
		`membership.professional_user_id = $1 AND connection.id = $2`,
		professionalUserID,
		connectionID,
	)
	if err != nil {
		return Connection{}, err
	}
	if len(connections) == 0 {
		return Connection{}, ErrNotFound
	}
	return connections[0], nil
}

func (r *Repository) listConnections(
	ctx context.Context,
	where string,
	arguments ...any,
) ([]Connection, error) {
	query := fmt.Sprintf(`
		SELECT connection.id::text, connection.status,
		       organization.id::text, organization.name,
		       professional.id::text, professional.display_name, profile.profession_type,
		       app_user.id::text, app_user.display_name, app_user.email,
		       COALESCE(array_agg(consent.scope ORDER BY consent.scope)
		           FILTER (WHERE consent.scope IS NOT NULL AND consent.revoked_at IS NULL), '{}'),
		       connection.activated_at, connection.ended_at, connection.created_at
		FROM professional_patient_connections AS connection
		JOIN organization_memberships AS membership
		  ON membership.id = connection.professional_membership_id
		JOIN organizations AS organization ON organization.id = connection.organization_id
		JOIN professional_users AS professional
		  ON professional.id = membership.professional_user_id
		JOIN professional_profiles AS profile
		  ON profile.professional_user_id = professional.id
		JOIN app_users AS app_user ON app_user.id = connection.app_user_id
		LEFT JOIN connection_consents AS consent ON consent.connection_id = connection.id
		WHERE %s
		GROUP BY connection.id, organization.id, professional.id, profile.profession_type, app_user.id
		ORDER BY COALESCE(connection.activated_at, connection.created_at) DESC
		LIMIT 200
	`, where)

	rows, err := r.pool.Query(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query care connections: %w", err)
	}
	defer rows.Close()

	connections := make([]Connection, 0)
	for rows.Next() {
		var connection Connection
		if err := rows.Scan(
			&connection.ID,
			&connection.Status,
			&connection.OrganizationID,
			&connection.OrganizationName,
			&connection.ProfessionalUserID,
			&connection.ProfessionalDisplayName,
			&connection.ProfessionType,
			&connection.AppUserID,
			&connection.PatientDisplayName,
			&connection.PatientEmail,
			&connection.ConsentScopes,
			&connection.ActivatedAt,
			&connection.EndedAt,
			&connection.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan care connection: %w", err)
		}
		connections = append(connections, connection)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate care connections: %w", err)
	}
	return connections, nil
}

func (r *Repository) ReplaceConnectionConsents(
	ctx context.Context,
	appUserID string,
	connectionID string,
	scopes []string,
	policyVersion string,
	client ClientInfo,
	now time.Time,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin connection consent transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM professional_patient_connections
			WHERE id = $1 AND app_user_id = $2 AND status = 'active'
		)
	`, connectionID, appUserID).Scan(&exists); err != nil {
		return fmt.Errorf("check consent connection: %w", err)
	}
	if !exists {
		return ErrNotFound
	}

	if _, err := tx.Exec(ctx, `
		UPDATE connection_consents
		SET revoked_at = $2
		WHERE connection_id = $1 AND revoked_at IS NULL
	`, connectionID, now); err != nil {
		return fmt.Errorf("revoke previous connection consents: %w", err)
	}
	for _, scope := range scopes {
		if _, err := tx.Exec(ctx, `
			INSERT INTO connection_consents (
				connection_id, scope, policy_version, granted_at, ip_address, user_agent
			)
			VALUES ($1, $2, $3, $4, NULLIF($5, '')::inet, NULLIF($6, ''))
		`, connectionID, scope, policyVersion, now, client.IPAddress, client.UserAgent); err != nil {
			return fmt.Errorf("replace connection consent: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit connection consent transaction: %w", err)
	}
	return nil
}

func (r *Repository) EndConnectionByApp(
	ctx context.Context,
	appUserID string,
	connectionID string,
	now time.Time,
) error {
	return r.endConnection(ctx, connectionID, "connection.app_user_id = $2", appUserID, now)
}

func (r *Repository) EndConnectionByProfessional(
	ctx context.Context,
	professionalUserID string,
	connectionID string,
	now time.Time,
) error {
	return r.endConnection(
		ctx,
		connectionID,
		"membership.professional_user_id = $2",
		professionalUserID,
		now,
	)
}

func (r *Repository) endConnection(
	ctx context.Context,
	connectionID string,
	ownershipPredicate string,
	ownerID string,
	now time.Time,
) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin ending connection: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	query := fmt.Sprintf(`
		UPDATE professional_patient_connections AS connection
		SET status = 'ended', ended_at = $3, updated_at = $3
		FROM organization_memberships AS membership
		WHERE connection.id = $1
		  AND connection.professional_membership_id = membership.id
		  AND %s
		  AND connection.status = 'active'
	`, ownershipPredicate)
	result, err := tx.Exec(ctx, query, connectionID, ownerID, now)
	if err != nil {
		return fmt.Errorf("end professional patient connection: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `
		UPDATE connection_consents
		SET revoked_at = $2
		WHERE connection_id = $1 AND revoked_at IS NULL
	`, connectionID, now); err != nil {
		return fmt.Errorf("revoke consents after connection end: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit ending connection: %w", err)
	}
	return nil
}
