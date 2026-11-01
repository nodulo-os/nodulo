package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	WebSearchProviderDuckDuckGo = "duckduckgo"
	WebSearchProviderSearXNG    = "searxng"
)

type WebSearchSettings struct {
	Enabled     bool      `json:"enabled"`
	Provider    string    `json:"provider"`
	Endpoint    string    `json:"endpoint"`
	ResultLimit int       `json:"result_limit"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type WebSearchSettingsRepository struct {
	pool *pgxpool.Pool
}

func NewWebSearchSettingsRepository(pool *pgxpool.Pool) *WebSearchSettingsRepository {
	return &WebSearchSettingsRepository{pool: pool}
}

func (r *WebSearchSettingsRepository) Get(ctx context.Context) (WebSearchSettings, error) {
	var settings WebSearchSettings
	err := r.pool.QueryRow(ctx, `SELECT enabled, provider, endpoint, result_limit, updated_at
		FROM web_search_settings WHERE id = TRUE`).Scan(
		&settings.Enabled, &settings.Provider, &settings.Endpoint, &settings.ResultLimit, &settings.UpdatedAt,
	)
	if err != nil {
		return WebSearchSettings{}, fmt.Errorf("get web search settings: %w", err)
	}
	return settings, nil
}

func (r *WebSearchSettingsRepository) Update(ctx context.Context, settings WebSearchSettings, actorID string) (WebSearchSettings, error) {
	_, err := r.pool.Exec(ctx, `INSERT INTO web_search_settings
		(id, enabled, provider, endpoint, result_limit, updated_by, updated_at)
		VALUES (TRUE, $1, $2, $3, $4, $5, CURRENT_TIMESTAMP)
		ON CONFLICT (id) DO UPDATE SET
			enabled = EXCLUDED.enabled,
			provider = EXCLUDED.provider,
			endpoint = EXCLUDED.endpoint,
			result_limit = EXCLUDED.result_limit,
			updated_by = EXCLUDED.updated_by,
			updated_at = EXCLUDED.updated_at`,
		settings.Enabled, settings.Provider, settings.Endpoint, settings.ResultLimit, actorID)
	if err != nil {
		return WebSearchSettings{}, fmt.Errorf("update web search settings: %w", err)
	}
	return r.Get(ctx)
}
