package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrRegistrationNotPending  = errors.New("solicitação de cadastro não está pendente")
	ErrAuthorizedEmailNotFound = errors.New("e-mail não encontrado na lista autorizada")
)

const (
	RegistrationModeAutomatic = "automatic"
	RegistrationModeApproval  = "approval"
	RegistrationPending       = "pending"
	RegistrationApproved      = "approved"
	RegistrationRejected      = "rejected"
)

type RegistrationSettings struct {
	Mode           string    `json:"mode"`
	AllowedDomains []string  `json:"allowed_domains"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type AuthorizedEmail struct {
	Email     string    `json:"email"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

type RegistrationRepository struct {
	pool *pgxpool.Pool
}

func NewRegistrationRepository(pool *pgxpool.Pool) *RegistrationRepository {
	return &RegistrationRepository{pool: pool}
}

func (r *RegistrationRepository) GetSettings(ctx context.Context) (RegistrationSettings, error) {
	settings := RegistrationSettings{AllowedDomains: make([]string, 0)}
	if err := r.pool.QueryRow(ctx, `SELECT mode, updated_at FROM registration_settings WHERE id = TRUE`).Scan(&settings.Mode, &settings.UpdatedAt); err != nil {
		return RegistrationSettings{}, fmt.Errorf("get registration settings: %w", err)
	}
	rows, err := r.pool.Query(ctx, `SELECT domain FROM registration_allowed_domains ORDER BY domain`)
	if err != nil {
		return RegistrationSettings{}, fmt.Errorf("list registration domains: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var domain string
		if err := rows.Scan(&domain); err != nil {
			return RegistrationSettings{}, fmt.Errorf("scan registration domain: %w", err)
		}
		settings.AllowedDomains = append(settings.AllowedDomains, domain)
	}
	if err := rows.Err(); err != nil {
		return RegistrationSettings{}, fmt.Errorf("iterate registration domains: %w", err)
	}
	return settings, nil
}

func (r *RegistrationRepository) UpdateSettings(ctx context.Context, mode string, domains []string, actorID string) (RegistrationSettings, error) {
	if mode != RegistrationModeAutomatic && mode != RegistrationModeApproval {
		return RegistrationSettings{}, fmt.Errorf("registration mode must be automatic or approval")
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return RegistrationSettings{}, fmt.Errorf("begin registration settings update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO registration_settings (id, mode, updated_by, updated_at)
		VALUES (TRUE, $1, $2, CURRENT_TIMESTAMP)
		ON CONFLICT (id) DO UPDATE SET mode = EXCLUDED.mode, updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`, mode, actorID); err != nil {
		return RegistrationSettings{}, fmt.Errorf("save registration mode: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM registration_allowed_domains`); err != nil {
		return RegistrationSettings{}, fmt.Errorf("replace registration domains: %w", err)
	}
	for _, domain := range domains {
		if _, err := tx.Exec(ctx, `INSERT INTO registration_allowed_domains (domain, created_by) VALUES ($1, $2)`, domain, actorID); err != nil {
			return RegistrationSettings{}, fmt.Errorf("save registration domain: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return RegistrationSettings{}, fmt.Errorf("commit registration settings: %w", err)
	}
	return r.GetSettings(ctx)
}

func (r *RegistrationRepository) IsEmailAuthorized(ctx context.Context, email string) (bool, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false, nil
	}
	var authorized bool
	err := r.pool.QueryRow(ctx, `SELECT
		EXISTS (SELECT 1 FROM registration_allowlist WHERE email = $1)
		OR EXISTS (SELECT 1 FROM registration_allowed_domains WHERE domain = $2)`, email, parts[1]).Scan(&authorized)
	if err != nil {
		return false, fmt.Errorf("check email registration authorization: %w", err)
	}
	return authorized, nil
}

func (r *RegistrationRepository) ListAuthorizedEmails(ctx context.Context) ([]AuthorizedEmail, error) {
	rows, err := r.pool.Query(ctx, `SELECT email, note, created_at FROM registration_allowlist ORDER BY email`)
	if err != nil {
		return nil, fmt.Errorf("list authorized emails: %w", err)
	}
	defer rows.Close()
	items := make([]AuthorizedEmail, 0)
	for rows.Next() {
		var item AuthorizedEmail
		if err := rows.Scan(&item.Email, &item.Note, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan authorized email: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate authorized emails: %w", err)
	}
	return items, nil
}

func (r *RegistrationRepository) AddAuthorizedEmails(ctx context.Context, emails []string, note, actorID string) (int, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin authorized email update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, email := range emails {
		if _, err := tx.Exec(ctx, `INSERT INTO registration_allowlist (email, note, created_by)
			VALUES ($1, $2, $3)
			ON CONFLICT (email) DO UPDATE SET note = EXCLUDED.note, created_by = EXCLUDED.created_by`, email, note, actorID); err != nil {
			return 0, fmt.Errorf("add authorized email: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit authorized email update: %w", err)
	}
	return len(emails), nil
}

func (r *RegistrationRepository) DeleteAuthorizedEmail(ctx context.Context, email string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM registration_allowlist WHERE email = $1`, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return fmt.Errorf("delete authorized email: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAuthorizedEmailNotFound
	}
	return nil
}

func (r *RegistrationRepository) ReviewUser(ctx context.Context, userID, actorID, status, reason string) (AdminUser, error) {
	if status != RegistrationApproved && status != RegistrationRejected {
		return AdminUser{}, fmt.Errorf("registration decision must be approved or rejected")
	}
	if status == RegistrationRejected && strings.TrimSpace(reason) == "" {
		return AdminUser{}, fmt.Errorf("rejection reason is required")
	}
	const query = `UPDATE users SET registration_status = $2, rejection_reason = $3, reviewed_by = $4, reviewed_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND registration_status = 'pending'
		RETURNING id::text, email, name, role, registration_status, COALESCE(rejection_reason, ''), created_at`
	var user AdminUser
	err := r.pool.QueryRow(ctx, query, userID, status, nullableReason(status, reason), actorID).Scan(
		&user.ID, &user.Email, &user.Name, &user.Role, &user.RegistrationStatus, &user.RejectionReason, &user.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminUser{}, ErrRegistrationNotPending
	}
	if err != nil {
		return AdminUser{}, fmt.Errorf("review user registration: %w", err)
	}
	return user, nil
}

func nullableReason(status, reason string) any {
	if status != RegistrationRejected {
		return nil
	}
	return strings.TrimSpace(reason)
}
