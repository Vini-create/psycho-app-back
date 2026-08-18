package auth

import "github.com/jackc/pgx/v5/pgxpool"

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func accountTable(audience Audience) (string, string, error) {
	switch audience {
	case AudienceApp:
		return "app_users", "app_user_id", nil
	case AudienceProfessional:
		return "professional_users", "professional_user_id", nil
	default:
		return "", "", ErrInvalidInput
	}
}
