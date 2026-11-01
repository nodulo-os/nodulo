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

var ErrMemoryDisabled = errors.New("semantic memory is disabled for this user")

type UserMemoryFact struct {
	ID        string    `json:"id"`
	Fact      string    `json:"fact"`
	CreatedAt time.Time `json:"created_at"`
}

// UserMemoryRepository stores durable, user-scoped profile facts and vectors.
type UserMemoryRepository struct {
	pool *pgxpool.Pool
}

func NewUserMemoryRepository(pool *pgxpool.Pool) *UserMemoryRepository {
	return &UserMemoryRepository{pool: pool}
}

func (r *UserMemoryRepository) InsertFact(ctx context.Context, userID, fact string, embedding []float32) error {
	if r == nil || r.pool == nil {
		return fmt.Errorf("user memory repository is not configured")
	}
	fact = strings.TrimSpace(fact)
	if fact == "" {
		return fmt.Errorf("memory fact is empty")
	}
	vector, err := formatEmbedding(embedding)
	if err != nil {
		return fmt.Errorf("format memory embedding: %w", err)
	}
	if len(embedding) != EmbeddingDimensions {
		return fmt.Errorf("memory embedding has %d dimensions; expected %d", len(embedding), EmbeddingDimensions)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin user memory insert: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var enabled bool
	if err := tx.QueryRow(ctx, `SELECT enabled FROM user_memory_settings WHERE user_id = $1 FOR UPDATE`, userID).Scan(&enabled); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMemoryDisabled
		}
		return fmt.Errorf("lock user memory preference: %w", err)
	}
	if !enabled {
		return ErrMemoryDisabled
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_memory (id, user_id, fact, embedding, created_at)
		VALUES (uuid_generate_v4(), $1, $2, $3::vector, CURRENT_TIMESTAMP)`, userID, fact, vector); err != nil {
		return fmt.Errorf("insert user memory fact: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit user memory fact: %w", err)
	}
	return nil
}

func (r *UserMemoryRepository) MemoryEnabled(ctx context.Context, userID string) (bool, error) {
	if r == nil || r.pool == nil {
		return false, fmt.Errorf("user memory repository is not configured")
	}
	var enabled bool
	err := r.pool.QueryRow(ctx, `SELECT enabled FROM user_memory_settings WHERE user_id = $1`, userID).Scan(&enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read user memory setting: %w", err)
	}
	return enabled, nil
}

func (r *UserMemoryRepository) SetMemoryEnabled(ctx context.Context, userID string, enabled bool) error {
	if r == nil || r.pool == nil {
		return fmt.Errorf("user memory repository is not configured")
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO user_memory_settings (user_id, enabled, updated_at) VALUES ($1, $2, CURRENT_TIMESTAMP)
		ON CONFLICT (user_id) DO UPDATE SET enabled = EXCLUDED.enabled, updated_at = CURRENT_TIMESTAMP`, userID, enabled)
	if err != nil {
		return fmt.Errorf("update user memory setting: %w", err)
	}
	return nil
}

func (r *UserMemoryRepository) ListFacts(ctx context.Context, userID string) ([]UserMemoryFact, error) {
	if r == nil || r.pool == nil {
		return nil, fmt.Errorf("user memory repository is not configured")
	}
	rows, err := r.pool.Query(ctx, `SELECT id::text, fact, created_at FROM user_memory WHERE user_id = $1 ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list user memory facts: %w", err)
	}
	defer rows.Close()
	facts := make([]UserMemoryFact, 0)
	for rows.Next() {
		var item UserMemoryFact
		if err := rows.Scan(&item.ID, &item.Fact, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan user memory fact: %w", err)
		}
		facts = append(facts, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user memory facts: %w", err)
	}
	return facts, nil
}

func (r *UserMemoryRepository) DeleteFact(ctx context.Context, userID, factID string) (bool, error) {
	if r == nil || r.pool == nil {
		return false, fmt.Errorf("user memory repository is not configured")
	}
	tag, err := r.pool.Exec(ctx, `DELETE FROM user_memory WHERE id = $1 AND user_id = $2`, factID, userID)
	if err != nil {
		return false, fmt.Errorf("delete user memory fact: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (r *UserMemoryRepository) DeleteAllFacts(ctx context.Context, userID string) (int64, error) {
	if r == nil || r.pool == nil {
		return 0, fmt.Errorf("user memory repository is not configured")
	}
	tag, err := r.pool.Exec(ctx, `DELETE FROM user_memory WHERE user_id = $1`, userID)
	if err != nil {
		return 0, fmt.Errorf("delete user memory facts: %w", err)
	}
	return tag.RowsAffected(), nil
}

// SearchRelevantFacts returns up to three semantically close facts for one user.
func (r *UserMemoryRepository) SearchRelevantFacts(ctx context.Context, userID string, embedding []float32, limit int) ([]string, error) {
	if r == nil || r.pool == nil {
		return nil, fmt.Errorf("user memory repository is not configured")
	}
	if len(embedding) != EmbeddingDimensions {
		return nil, fmt.Errorf("query embedding has %d dimensions; expected %d", len(embedding), EmbeddingDimensions)
	}
	if limit <= 0 || limit > 3 {
		limit = 3
	}
	vector, err := formatEmbedding(embedding)
	if err != nil {
		return nil, fmt.Errorf("format query embedding: %w", err)
	}
	const query = `
		SELECT fact
		FROM user_memory
		WHERE user_id = $1
		  AND EXISTS (SELECT 1 FROM user_memory_settings WHERE user_id = $1 AND enabled = TRUE)
		ORDER BY embedding <=> $2::vector ASC, created_at DESC
		LIMIT $3`
	rows, err := r.pool.Query(ctx, query, userID, vector, limit)
	if err != nil {
		return nil, fmt.Errorf("search user memory: %w", err)
	}
	defer rows.Close()
	facts := make([]string, 0, limit)
	for rows.Next() {
		var fact string
		if err := rows.Scan(&fact); err != nil {
			return nil, fmt.Errorf("scan user memory fact: %w", err)
		}
		facts = append(facts, fact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user memory facts: %w", err)
	}
	return facts, nil
}
