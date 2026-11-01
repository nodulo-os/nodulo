package repository

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type IngestionJob struct {
	ID              string    `json:"id"`
	Filename        string    `json:"filename"`
	Title           string    `json:"title,omitempty"`
	SourceReference string    `json:"source_reference"`
	FileType        string    `json:"file_type"`
	Status          string    `json:"status"`
	TotalChunks     int       `json:"total_chunks"`
	ProcessedChunks int       `json:"processed_chunks"`
	ProgressPercent float64   `json:"progress_percent"`
	ErrorMessage    string    `json:"error_message,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	PayloadPath     string    `json:"-"`
	SourceURL       string    `json:"-"`
	LeaseOwner      string    `json:"-"`
}

type IngestionRepository struct {
	pool *pgxpool.Pool
}

var ErrJobNotRetryable = errors.New("ingestion job is not retryable")

func NewIngestionRepository(pool *pgxpool.Pool) *IngestionRepository {
	return &IngestionRepository{pool: pool}
}

func (r *IngestionRepository) CreateJob(ctx context.Context, filename, sourceRef, fileType string) (IngestionJob, error) {
	return r.CreateJobWithTitle(ctx, filename, "", sourceRef, fileType)
}

func (r *IngestionRepository) CreateJobWithTitle(ctx context.Context, filename, title, sourceRef, fileType string) (IngestionJob, error) {
	return r.CreateJobWithPayload(ctx, filename, title, sourceRef, fileType, "", "")
}

func (r *IngestionRepository) CreateJobWithPayload(ctx context.Context, filename, title, sourceRef, fileType, payloadPath, sourceURL string) (IngestionJob, error) {
	jobs, err := r.CreateJobsWithPayload(ctx, []IngestionJob{{
		Filename: filename, Title: title, SourceReference: sourceRef, FileType: fileType,
		PayloadPath: payloadPath, SourceURL: sourceURL,
	}})
	if err != nil {
		return IngestionJob{}, err
	}
	return jobs[0], nil
}

// CreateJobsWithPayload makes a batch visible to workers atomically, so a
// worker cannot start the first upload while the rest of the batch is staged.
func (r *IngestionRepository) CreateJobsWithPayload(ctx context.Context, input []IngestionJob) ([]IngestionJob, error) {
	if len(input) == 0 {
		return []IngestionJob{}, nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin ingestion batch: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	const query = `
		INSERT INTO ingestion_jobs (filename, title, source_reference, file_type, payload_path, source_url)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''))
		RETURNING id::text, filename, title, source_reference, file_type, status,
			total_chunks, processed_chunks, COALESCE(error_message, ''), created_at, updated_at,
			COALESCE(payload_path, ''), COALESCE(source_url, ''), COALESCE(lease_owner, '')`
	jobs := make([]IngestionJob, 0, len(input))
	for _, item := range input {
		job, err := scanIngestionJob(tx.QueryRow(ctx, query, item.Filename, item.Title, item.SourceReference, item.FileType, item.PayloadPath, item.SourceURL))
		if err != nil {
			return nil, fmt.Errorf("create ingestion job: %w", err)
		}
		jobs = append(jobs, job)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit ingestion batch: %w", err)
	}
	return jobs, nil
}

func (r *IngestionRepository) GetJob(ctx context.Context, jobID string) (IngestionJob, error) {
	job, err := scanIngestionJob(r.pool.QueryRow(ctx, `
		SELECT id::text, filename, title, source_reference, file_type, status,
			total_chunks, processed_chunks, COALESCE(error_message, ''), created_at, updated_at,
			COALESCE(payload_path, ''), COALESCE(source_url, ''), COALESCE(lease_owner, '')
		FROM ingestion_jobs WHERE id = $1`, jobID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IngestionJob{}, ErrNotFound
		}
		return IngestionJob{}, fmt.Errorf("get ingestion job: %w", err)
	}
	job.ProgressPercent = progressPercent(job)
	return job, nil
}

// PrepareRetry only requeues failed jobs and preserves their durable progress.
func (r *IngestionRepository) PrepareRetry(ctx context.Context, jobID, payloadPath string) (IngestionJob, error) {
	job, err := scanIngestionJob(r.pool.QueryRow(ctx, `
		UPDATE ingestion_jobs
		SET status = 'pending', error_message = NULL, payload_path = NULLIF($2, ''),
			lease_owner = NULL, lease_expires_at = NULL, updated_at = NOW()
		WHERE id = $1 AND status = 'failed'
		RETURNING id::text, filename, title, source_reference, file_type, status,
			total_chunks, processed_chunks, COALESCE(error_message, ''), created_at, updated_at,
			COALESCE(payload_path, ''), COALESCE(source_url, ''), COALESCE(lease_owner, '')`, jobID, payloadPath))
	if err == nil {
		job.ProgressPercent = progressPercent(job)
		return job, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return IngestionJob{}, fmt.Errorf("prepare ingestion retry: %w", err)
	}
	if _, lookupErr := r.GetJob(ctx, jobID); errors.Is(lookupErr, ErrNotFound) {
		return IngestionJob{}, ErrNotFound
	} else if lookupErr != nil {
		return IngestionJob{}, lookupErr
	}
	return IngestionJob{}, ErrJobNotRetryable
}

// ClaimNextJob atomically leases one durable job. SKIP LOCKED allows multiple
// API instances to share the PostgreSQL queue without claiming the same task.
func (r *IngestionRepository) ClaimNextJob(ctx context.Context, owner string, lease time.Duration) (IngestionJob, bool, error) {
	if lease <= 0 {
		lease = 5 * time.Minute
	}
	job, err := scanIngestionJob(r.pool.QueryRow(ctx, `
		WITH candidate AS (
			SELECT id FROM ingestion_jobs
			WHERE status = 'pending'
			   OR (status = 'processing' AND COALESCE(lease_expires_at, to_timestamp(0)) <= NOW())
			ORDER BY created_at ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE ingestion_jobs AS jobs
		SET status = 'processing', error_message = NULL, lease_owner = $1,
			lease_expires_at = NOW() + (($2::double precision / 1000) * INTERVAL '1 second'), updated_at = NOW()
		FROM candidate
		WHERE jobs.id = candidate.id
		RETURNING jobs.id::text, jobs.filename, jobs.title, jobs.source_reference, jobs.file_type, jobs.status,
			jobs.total_chunks, jobs.processed_chunks, COALESCE(jobs.error_message, ''), jobs.created_at, jobs.updated_at,
			COALESCE(jobs.payload_path, ''), COALESCE(jobs.source_url, ''), COALESCE(jobs.lease_owner, '')`, owner, lease.Milliseconds()))
	if errors.Is(err, ErrNotFound) {
		return IngestionJob{}, false, nil
	}
	if err != nil {
		return IngestionJob{}, false, fmt.Errorf("claim next ingestion job: %w", err)
	}
	job.ProgressPercent = progressPercent(job)
	return job, true, nil
}

func (r *IngestionRepository) HeartbeatJob(ctx context.Context, jobID, owner string, lease time.Duration) (bool, error) {
	if lease <= 0 {
		lease = 5 * time.Minute
	}
	tag, err := r.pool.Exec(ctx, `UPDATE ingestion_jobs SET lease_expires_at = NOW() + (($3::double precision / 1000) * INTERVAL '1 second'), updated_at = NOW() WHERE id = $1 AND status = 'processing' AND lease_owner = $2`, jobID, owner, lease.Milliseconds())
	if err != nil {
		return false, fmt.Errorf("renew ingestion job lease: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *IngestionRepository) ReleaseJob(ctx context.Context, jobID, owner string) error {
	_, err := r.pool.Exec(ctx, `UPDATE ingestion_jobs SET status = 'pending', lease_owner = NULL, lease_expires_at = NULL, updated_at = NOW() WHERE id = $1 AND status = 'processing' AND lease_owner = $2`, jobID, owner)
	if err != nil {
		return fmt.Errorf("release ingestion job lease: %w", err)
	}
	return nil
}

func (r *IngestionRepository) UpdateProgress(ctx context.Context, jobID, owner string, lease time.Duration, total, processed int) error {
	if total < 0 || processed < 0 || processed > total {
		return fmt.Errorf("invalid ingestion progress %d/%d", processed, total)
	}
	if lease <= 0 {
		lease = 90 * time.Second
	}
	return r.updateJob(ctx, "update ingestion progress", `
		UPDATE ingestion_jobs
		SET total_chunks = $3, processed_chunks = $4, updated_at = NOW(),
			lease_expires_at = NOW() + (($5::double precision / 1000) * INTERVAL '1 second')
		WHERE id = $1 AND lease_owner = $2 AND status = 'processing'`, jobID, owner, total, processed, lease.Milliseconds())
}

func (r *IngestionRepository) Complete(ctx context.Context, jobID, owner string) error {
	return r.updateJob(ctx, "complete ingestion job", `
		UPDATE ingestion_jobs
		SET status = 'completed', processed_chunks = total_chunks, error_message = NULL,
			payload_path = NULL, lease_owner = NULL, lease_expires_at = NULL, updated_at = NOW()
		WHERE id = $1 AND lease_owner = $2 AND status = 'processing'`, jobID, owner)
}

func (r *IngestionRepository) FailClaimed(ctx context.Context, jobID, owner string, cause error) error {
	message := "falha não especificada"
	if cause != nil {
		message = strings.TrimSpace(cause.Error())
	}
	if runes := []rune(message); len(runes) > 4000 {
		message = string(runes[:4000])
	}
	return r.updateJob(ctx, "fail claimed ingestion job", `
		UPDATE ingestion_jobs SET status = 'failed', error_message = $3, lease_owner = NULL,
			lease_expires_at = NULL, updated_at = NOW()
		WHERE id = $1 AND lease_owner = $2 AND status = 'processing'`, jobID, owner, message)
}

func (r *IngestionRepository) Fail(ctx context.Context, jobID string, cause error) error {
	message := "falha não especificada"
	if cause != nil {
		message = strings.TrimSpace(cause.Error())
	}
	if runes := []rune(message); len(runes) > 4000 {
		message = string(runes[:4000])
	}
	return r.updateJob(ctx, "fail ingestion job", `
		UPDATE ingestion_jobs SET status = 'failed', error_message = $2, lease_owner = NULL, lease_expires_at = NULL, updated_at = NOW()
		WHERE id = $1`, jobID, message)
}

func (r *IngestionRepository) ListJobs(ctx context.Context, activeOnly bool) ([]IngestionJob, error) {
	query := `
		SELECT id::text, filename, title, source_reference, file_type, status,
			total_chunks, processed_chunks, COALESCE(error_message, ''), created_at, updated_at,
			COALESCE(payload_path, ''), COALESCE(source_url, ''), COALESCE(lease_owner, '')
		FROM ingestion_jobs
		ORDER BY created_at DESC
		LIMIT 200`
	if activeOnly {
		query = `
			SELECT id::text, filename, title, source_reference, file_type, status,
				total_chunks, processed_chunks, COALESCE(error_message, ''), created_at, updated_at,
				COALESCE(payload_path, ''), COALESCE(source_url, ''), COALESCE(lease_owner, '')
			FROM ingestion_jobs
			WHERE status IN ('pending', 'processing')
			ORDER BY created_at DESC
			LIMIT 200`
	}
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list ingestion jobs: %w", err)
	}
	defer rows.Close()
	jobs := make([]IngestionJob, 0)
	for rows.Next() {
		job, err := scanIngestionJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan ingestion job: %w", err)
		}
		job.ProgressPercent = progressPercent(job)
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ingestion jobs: %w", err)
	}
	return jobs, nil
}

func (r *IngestionRepository) updateJob(ctx context.Context, operation, query string, args ...any) error {
	tag, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanIngestionJob(row interface{ Scan(...any) error }) (IngestionJob, error) {
	var job IngestionJob
	err := row.Scan(
		&job.ID, &job.Filename, &job.Title, &job.SourceReference, &job.FileType, &job.Status,
		&job.TotalChunks, &job.ProcessedChunks, &job.ErrorMessage, &job.CreatedAt, &job.UpdatedAt,
		&job.PayloadPath, &job.SourceURL, &job.LeaseOwner,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IngestionJob{}, ErrNotFound
		}
		return IngestionJob{}, err
	}
	return job, nil
}

func progressPercent(job IngestionJob) float64 {
	if job.Status == "completed" {
		return 100
	}
	if job.TotalChunks <= 0 {
		return 0
	}
	percent := float64(job.ProcessedChunks) / float64(job.TotalChunks) * 100
	return math.Round(percent*100) / 100
}
