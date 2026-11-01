package repository

import (
	"context"
	"fmt"
	"strings"

	"core-nodulo/internal/observability"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ErrorLogPage struct {
	Items      []observability.ErrorEvent `json:"items"`
	HasMore    bool                       `json:"has_more"`
	NextCursor int64                      `json:"next_cursor,omitempty"`
}

type SystemLogRepository struct{ pool *pgxpool.Pool }

func NewSystemLogRepository(pool *pgxpool.Pool) *SystemLogRepository {
	return &SystemLogRepository{pool: pool}
}

func (r *SystemLogRepository) InsertErrorLog(ctx context.Context, event observability.ErrorEvent) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO system_error_logs
			(occurred_at, level, source, message, details, request_id, actor_user_id, method, route, status_code)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::uuid, NULLIF($7, '')::uuid, $8, $9, $10)`,
		event.Time, event.Level, event.Source, event.Message, event.Details,
		event.RequestID, event.ActorUserID, event.Method, event.Route, event.StatusCode)
	if err != nil {
		return fmt.Errorf("persist system error log: %w", err)
	}
	return nil
}

func (r *SystemLogRepository) ListErrorLogs(ctx context.Context, query, level string, beforeID int64, limit int) (ErrorLogPage, error) {
	if limit < 1 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	query = strings.TrimSpace(query)
	level = strings.ToUpper(strings.TrimSpace(level))
	if level != "WARN" && level != "ERROR" {
		level = ""
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, occurred_at, level, source, message, details, request_id,
			COALESCE(actor_user_id::text, ''), method, route, status_code
		FROM system_error_logs
		WHERE ($1 = '' OR level = $1)
		  AND ($2 = '' OR message ILIKE '%' || $2 || '%' OR details ILIKE '%' || $2 || '%'
		       OR request_id ILIKE '%' || $2 || '%' OR COALESCE(actor_user_id::text, '') ILIKE '%' || $2 || '%'
		       OR route ILIKE '%' || $2 || '%')
		  AND ($3 <= 0 OR id < $3)
		ORDER BY id DESC
		LIMIT $4`, level, query, beforeID, limit+1)
	if err != nil {
		return ErrorLogPage{}, fmt.Errorf("list system error logs: %w", err)
	}
	defer rows.Close()
	items := make([]observability.ErrorEvent, 0, limit+1)
	for rows.Next() {
		var item observability.ErrorEvent
		if err := rows.Scan(&item.ID, &item.Time, &item.Level, &item.Source, &item.Message, &item.Details,
			&item.RequestID, &item.ActorUserID, &item.Method, &item.Route, &item.StatusCode); err != nil {
			return ErrorLogPage{}, fmt.Errorf("scan system error log: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ErrorLogPage{}, fmt.Errorf("iterate system error logs: %w", err)
	}
	page := ErrorLogPage{Items: items}
	if len(items) > limit {
		page.HasMore = true
		page.Items = items[:limit]
	}
	if len(page.Items) > 0 {
		page.NextCursor = page.Items[len(page.Items)-1].ID
	}
	return page, nil
}
