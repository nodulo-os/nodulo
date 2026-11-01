package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrLastAdmin        = errors.New("não é possível remover ou rebaixar o último administrador")
	ErrCannotDeleteSelf = errors.New("não é possível remover a própria conta")
	ErrInvalidRole      = errors.New("papel de usuário inválido")
)

const userAdminMutationLockID int64 = 0x4e4f44554c5553

type SystemStats struct {
	DatabaseSize       string `json:"database_size"`
	EmbeddingsSize     string `json:"embeddings_size"`
	TotalUsers         int    `json:"total_users"`
	TotalConversations int    `json:"total_conversations"`
	TotalMessages      int    `json:"total_messages"`
	TotalChunks        int    `json:"total_chunks"`
}

type IndexedDocument struct {
	Title           string    `json:"title"`
	SourceReference string    `json:"source_reference"`
	ChunksCount     int       `json:"chunks_count"`
	LastIngestedAt  time.Time `json:"last_ingested_at"`
}

type AdminUser struct {
	ID                 string    `json:"id"`
	Email              string    `json:"email"`
	Name               string    `json:"name"`
	Role               string    `json:"role"`
	RegistrationStatus string    `json:"registration_status"`
	RejectionReason    string    `json:"rejection_reason,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
}

type AdminRepository struct {
	pool *pgxpool.Pool
}

func NewAdminRepository(pool *pgxpool.Pool) *AdminRepository {
	return &AdminRepository{pool: pool}
}

func (r *AdminRepository) GetStats(ctx context.Context) (SystemStats, error) {
	const query = `
		SELECT
			pg_size_pretty(pg_database_size(current_database())),
			pg_size_pretty(pg_total_relation_size('document_chunks')),
			(SELECT COUNT(*) FROM users),
			(SELECT COUNT(*) FROM conversations),
			(SELECT COUNT(*) FROM messages),
			(SELECT COUNT(*) FROM document_chunks)`

	var stats SystemStats
	if err := r.pool.QueryRow(ctx, query).Scan(
		&stats.DatabaseSize,
		&stats.EmbeddingsSize,
		&stats.TotalUsers,
		&stats.TotalConversations,
		&stats.TotalMessages,
		&stats.TotalChunks,
	); err != nil {
		return SystemStats{}, fmt.Errorf("get administration statistics: %w", err)
	}
	return stats, nil
}

func (r *AdminRepository) ListIndexedDocuments(ctx context.Context) ([]IndexedDocument, error) {
	const query = `
		SELECT title, source_reference, COUNT(*)::integer, MAX(created_at)
		FROM document_chunks
		GROUP BY source_reference, title
		ORDER BY MAX(created_at) DESC, title ASC, source_reference ASC`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list indexed documents: %w", err)
	}
	defer rows.Close()

	documents := make([]IndexedDocument, 0)
	for rows.Next() {
		var document IndexedDocument
		if err := rows.Scan(&document.Title, &document.SourceReference, &document.ChunksCount, &document.LastIngestedAt); err != nil {
			return nil, fmt.Errorf("scan indexed document: %w", err)
		}
		documents = append(documents, document)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate indexed documents: %w", err)
	}
	return documents, nil
}

func (r *AdminRepository) DeleteDocumentBySource(ctx context.Context, sourceRef string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM document_chunks WHERE source_reference = $1`, sourceRef)
	if err != nil {
		return fmt.Errorf("delete indexed document by source: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *AdminRepository) ListUsers(ctx context.Context) ([]AdminUser, error) {
	const query = `
		SELECT id::text, email, name, role, registration_status, COALESCE(rejection_reason, ''), created_at
		FROM users
		ORDER BY created_at DESC, email ASC`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	users := make([]AdminUser, 0)
	for rows.Next() {
		var user AdminUser
		if err := rows.Scan(&user.ID, &user.Email, &user.Name, &user.Role, &user.RegistrationStatus, &user.RejectionReason, &user.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}
	return users, nil
}

func (r *AdminRepository) UpdateUserRole(ctx context.Context, userID, role string) (AdminUser, error) {
	if role != "admin" && role != "user" {
		return AdminUser{}, ErrInvalidRole
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return AdminUser{}, fmt.Errorf("begin user role update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, userAdminMutationLockID); err != nil {
		return AdminUser{}, fmt.Errorf("lock administrator changes: %w", err)
	}

	var currentRole string
	if err := tx.QueryRow(ctx, `SELECT role FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&currentRole); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AdminUser{}, ErrNotFound
		}
		return AdminUser{}, fmt.Errorf("read user role: %w", err)
	}
	if currentRole == "admin" && role != "admin" {
		var adminCount int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&adminCount); err != nil {
			return AdminUser{}, fmt.Errorf("count administrators: %w", err)
		}
		if adminCount <= 1 {
			return AdminUser{}, ErrLastAdmin
		}
	}

	var user AdminUser
	if err := tx.QueryRow(ctx, `
		UPDATE users SET role = $2 WHERE id = $1
		RETURNING id::text, email, name, role, registration_status, COALESCE(rejection_reason, ''), created_at`, userID, role).Scan(
		&user.ID, &user.Email, &user.Name, &user.Role, &user.RegistrationStatus, &user.RejectionReason, &user.CreatedAt,
	); err != nil {
		return AdminUser{}, fmt.Errorf("update user role: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminUser{}, fmt.Errorf("commit user role update: %w", err)
	}
	return user, nil
}

func (r *AdminRepository) DeleteUser(ctx context.Context, actorID, userID string) error {
	if actorID == userID {
		return ErrCannotDeleteSelf
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin user deletion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, userAdminMutationLockID); err != nil {
		return fmt.Errorf("lock administrator changes: %w", err)
	}

	var role string
	if err := tx.QueryRow(ctx, `SELECT role FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("read user before deletion: %w", err)
	}
	if role == "admin" {
		var adminCount int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&adminCount); err != nil {
			return fmt.Errorf("count administrators: %w", err)
		}
		if adminCount <= 1 {
			return ErrLastAdmin
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit user deletion: %w", err)
	}
	return nil
}
