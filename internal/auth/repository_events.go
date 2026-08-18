package auth

import (
	"context"
	"fmt"
)

func (r *Repository) RecordEvent(ctx context.Context, event Event) error {
	var appUserID any
	var professionalUserID any
	if event.AccountID != "" {
		if event.Audience == AudienceApp {
			appUserID = event.AccountID
		} else if event.Audience == AudienceProfessional {
			professionalUserID = event.AccountID
		}
	}

	if _, err := r.pool.Exec(ctx, `
		INSERT INTO auth_events (
			audience,
			app_user_id,
			professional_user_id,
			session_id,
			event_type,
			outcome,
			ip_address,
			user_agent
		)
		VALUES (
			$1,
			$2,
			$3,
			NULLIF($4, '')::uuid,
			$5,
			$6,
			NULLIF($7, '')::inet,
			NULLIF($8, '')
		)
	`,
		event.Audience,
		appUserID,
		professionalUserID,
		event.SessionID,
		event.Type,
		event.Outcome,
		event.Client.IPAddress,
		event.Client.UserAgent,
	); err != nil {
		return fmt.Errorf("record auth event: %w", err)
	}

	return nil
}
