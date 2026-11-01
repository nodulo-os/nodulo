package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ChatUploadSettings defines per-installation limits. A value of zero means
// that the corresponding limit is disabled.
type ChatUploadSettings struct {
	MaxFiles        int       `json:"max_files"`
	MaxFileSizeMiB  int64     `json:"max_file_size_mib"`
	MaxTotalSizeMiB int64     `json:"max_total_size_mib"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type ChatUploadSettingsRepository struct {
	pool *pgxpool.Pool
}

func NewChatUploadSettingsRepository(pool *pgxpool.Pool) *ChatUploadSettingsRepository {
	return &ChatUploadSettingsRepository{pool: pool}
}

func (r *ChatUploadSettingsRepository) Get(ctx context.Context) (ChatUploadSettings, error) {
	var settings ChatUploadSettings
	err := r.pool.QueryRow(ctx, `SELECT max_files, max_file_size_mib, max_total_size_mib, updated_at
		FROM chat_upload_settings WHERE id = TRUE`).Scan(
		&settings.MaxFiles, &settings.MaxFileSizeMiB, &settings.MaxTotalSizeMiB, &settings.UpdatedAt,
	)
	if err != nil {
		return ChatUploadSettings{}, fmt.Errorf("get chat upload settings: %w", err)
	}
	return settings, nil
}

func (r *ChatUploadSettingsRepository) Update(ctx context.Context, settings ChatUploadSettings, actorID string) (ChatUploadSettings, error) {
	_, err := r.pool.Exec(ctx, `INSERT INTO chat_upload_settings
		(id, max_files, max_file_size_mib, max_total_size_mib, updated_by, updated_at)
		VALUES (TRUE, $1, $2, $3, $4, CURRENT_TIMESTAMP)
		ON CONFLICT (id) DO UPDATE SET
		max_files = EXCLUDED.max_files,
		max_file_size_mib = EXCLUDED.max_file_size_mib,
		max_total_size_mib = EXCLUDED.max_total_size_mib,
		updated_by = EXCLUDED.updated_by,
		updated_at = EXCLUDED.updated_at`,
		settings.MaxFiles, settings.MaxFileSizeMiB, settings.MaxTotalSizeMiB, actorID)
	if err != nil {
		return ChatUploadSettings{}, fmt.Errorf("update chat upload settings: %w", err)
	}
	return r.Get(ctx)
}
