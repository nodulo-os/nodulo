package repository

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

var ErrEmailAlreadyExists = errors.New("e-mail já cadastrado")

type User struct {
	ID                 string    `json:"id"`
	Email              string    `json:"email"`
	Name               string    `json:"name"`
	Role               string    `json:"role"`
	RegistrationStatus string    `json:"registration_status"`
	RejectionReason    string    `json:"-"`
	PasswordHash       string    `json:"-"`
	CreatedAt          time.Time `json:"created_at"`
}

type UserRepository struct {
	pool *pgxpool.Pool
}

func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

func (r *UserRepository) CreateUser(ctx context.Context, name, email, passwordHash, role, registrationStatus string) (*User, error) {
	const query = `
		INSERT INTO users (id, name, email, password_hash, role, registration_status, created_at)
		VALUES (uuid_generate_v4(), $1, $2, $3, $4, $5, CURRENT_TIMESTAMP)
		RETURNING id::text, email, name, role, registration_status, COALESCE(rejection_reason, ''), password_hash, created_at`
	user, err := scanUser(r.pool.QueryRow(ctx, query, name, email, passwordHash, role, registrationStatus))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrEmailAlreadyExists
		}
		return nil, fmt.Errorf("create user: %w", err)
	}
	return user, nil
}

func (r *UserRepository) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	const query = `
		SELECT id::text, email, name, role, registration_status, COALESCE(rejection_reason, ''), COALESCE(password_hash, ''), created_at
		FROM users WHERE email = $1`
	user, err := scanUser(r.pool.QueryRow(ctx, query, strings.ToLower(strings.TrimSpace(email))))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user by email: %w", err)
	}
	return user, nil
}

func (r *UserRepository) GetUserByID(ctx context.Context, id string) (*User, error) {
	const query = `
		SELECT id::text, email, name, role, registration_status, COALESCE(rejection_reason, ''), COALESCE(password_hash, ''), created_at
		FROM users WHERE id = $1`
	user, err := scanUser(r.pool.QueryRow(ctx, query, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user by id: %w", err)
	}
	return user, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row rowScanner) (*User, error) {
	var user User
	err := row.Scan(&user.ID, &user.Email, &user.Name, &user.Role, &user.RegistrationStatus, &user.RejectionReason, &user.PasswordHash, &user.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &user, nil
}
