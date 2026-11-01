package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"core-nodulo/internal/observability"
	"core-nodulo/internal/repository"
	"core-nodulo/pkg/extractor"
)

var ErrIngestionQueueFull = errors.New("fila de ingestão cheia") // Retained for API compatibility; queue capacity is now PostgreSQL-backed.

const ingestionLeaseDuration = 90 * time.Second

type IngestionTask struct {
	JobID           string
	Filename        string
	Title           string
	SourceReference string
	FileType        string
	TempPath        string
	URL             string
	ResumeFrom      int
}

type JobWorker struct {
	jobs        *repository.IngestionRepository
	rag         *RAGService
	wake        chan struct{}
	workerCount int
	workerID    string
	startOnce   sync.Once
	mu          sync.Mutex
	started     bool
	closing     bool
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	wg          sync.WaitGroup
}

func NewJobWorker(jobs *repository.IngestionRepository, rag *RAGService, workerCount, _ int) *JobWorker {
	if workerCount < 1 {
		workerCount = 1
	}
	var identity [16]byte
	if _, err := rand.Read(identity[:]); err != nil {
		// The value is only a database lease owner; PID plus timestamp still
		// makes accidental collisions across API processes highly unlikely.
		copy(identity[:], []byte(fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())))
	}
	return &JobWorker{
		jobs: jobs, rag: rag, wake: make(chan struct{}, 1), workerCount: workerCount,
		workerID: hex.EncodeToString(identity[:]), done: make(chan struct{}),
	}
}

func (w *JobWorker) Start(parent context.Context) {
	w.startOnce.Do(func() {
		ctx, cancel := context.WithCancel(parent)
		w.mu.Lock()
		w.ctx, w.cancel, w.started = ctx, cancel, true
		w.mu.Unlock()
		for index := 0; index < w.workerCount; index++ {
			w.wg.Add(1)
			go w.run(ctx)
		}
		go func() { w.wg.Wait(); close(w.done) }()
		w.signal()
	})
}

// Enqueue is a wake-up hint only. Job details and uploaded files are committed
// durably before this call; workers always claim their next task from PostgreSQL.
func (w *JobWorker) Enqueue(_ IngestionTask) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.started {
		return errors.New("worker de ingestão não foi iniciado")
	}
	if w.closing || w.ctx.Err() != nil {
		return errors.New("worker de ingestão está encerrando")
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
	return nil
}

func (w *JobWorker) signal() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *JobWorker) Shutdown(ctx context.Context) error {
	w.mu.Lock()
	if !w.started {
		w.mu.Unlock()
		return nil
	}
	w.closing = true
	w.mu.Unlock()
	w.signal()
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		w.cancel()
		<-w.done
		return ctx.Err()
	}
}

func (w *JobWorker) run(ctx context.Context) {
	defer w.wg.Done()
	poll := time.NewTicker(time.Second)
	defer poll.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		job, claimed, err := w.jobs.ClaimNextJob(ctx, w.workerID, ingestionLeaseDuration)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.ErrorContext(ctx, "claim durable ingestion job failed", "error", observability.SafeError(err))
		} else if claimed {
			w.process(ctx, job)
			continue
		} else if w.isClosing() {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
		case <-poll.C:
		}
	}
}

func (w *JobWorker) isClosing() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closing
}

func (w *JobWorker) process(parent context.Context, job repository.IngestionJob) {
	startedAt := time.Now()
	task := IngestionTask{
		JobID: job.ID, Filename: job.Filename, Title: job.Title,
		SourceReference: job.SourceReference, FileType: job.FileType,
		TempPath: job.PayloadPath, URL: job.SourceURL, ResumeFrom: job.ProcessedChunks,
	}
	slog.InfoContext(parent, "ingestion_job_started", "job_id", task.JobID, "file_type", task.FileType, "resume_from", task.ResumeFrom)
	ctx, cancel := context.WithCancel(parent)
	heartbeatDone := make(chan struct{})
	go w.maintainLease(ctx, cancel, task.JobID, heartbeatDone)
	defer func() { cancel(); <-heartbeatDone }()

	var documents []extractor.ExtractedDocument
	if task.FileType == "url" {
		text, err := (extractor.WebExtractor{}).ExtractURL(ctx, task.URL)
		if err != nil {
			w.failOrRelease(ctx, task.JobID, err)
			return
		}
		documents = []extractor.ExtractedDocument{{Name: task.Filename, Content: text}}
	} else {
		if task.TempPath == "" {
			w.failOrRelease(ctx, task.JobID, errors.New("arquivo de origem não está registrado no armazenamento persistente"))
			return
		}
		file, err := os.Open(task.TempPath)
		if err != nil {
			w.failOrRelease(ctx, task.JobID, fmt.Errorf("abrir arquivo persistente: %w", err))
			return
		}
		info, statErr := file.Stat()
		if statErr != nil {
			_ = file.Close()
			w.failOrRelease(ctx, task.JobID, fmt.Errorf("consultar arquivo persistente: %w", statErr))
			return
		}
		documents, err = extractor.ExtractFile(task.Filename, file, info.Size())
		_ = file.Close()
		if err != nil {
			w.failOrRelease(ctx, task.JobID, err)
			return
		}
	}

	ragDocuments := make([]IngestDocument, 0, len(documents))
	for _, document := range documents {
		title := strings.TrimSpace(strings.TrimSuffix(filepath.Base(document.Name), filepath.Ext(document.Name)))
		if title == "" {
			title = strings.TrimSpace(task.Filename)
		}
		if len(documents) == 1 && strings.TrimSpace(task.Title) != "" {
			title = strings.TrimSpace(task.Title)
		}
		ragDocuments = append(ragDocuments, IngestDocument{Title: title, Content: document.Content})
	}
	chunksCount, err := w.rag.ResumeIngestDocuments(ctx, task.SourceReference, ragDocuments, task.ResumeFrom, func(total, processed int) error {
		return w.jobs.UpdateProgress(ctx, task.JobID, w.workerID, ingestionLeaseDuration, total, processed)
	})
	if err != nil {
		w.failOrRelease(ctx, task.JobID, err)
		return
	}
	if err := w.jobs.Complete(ctx, task.JobID, w.workerID); err != nil {
		w.failOrRelease(ctx, task.JobID, err)
		return
	}
	if task.TempPath != "" {
		_ = os.Remove(task.TempPath)
	}
	slog.InfoContext(ctx, "ingestion_job_completed", "job_id", task.JobID, "chunks_count", chunksCount, "elapsed_ms", time.Since(startedAt).Milliseconds())
}

func (w *JobWorker) maintainLease(ctx context.Context, cancel context.CancelFunc, jobID string, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			renewed, err := w.jobs.HeartbeatJob(ctx, jobID, w.workerID, ingestionLeaseDuration)
			if err != nil {
				slog.WarnContext(ctx, "renew ingestion job lease failed", "job_id", jobID, "error", observability.SafeError(err))
				continue
			}
			if !renewed {
				slog.WarnContext(ctx, "ingestion job lease lost", "job_id", jobID)
				cancel()
				return
			}
		}
	}
}

func (w *JobWorker) failOrRelease(ctx context.Context, jobID string, cause error) {
	if ctx.Err() != nil {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := w.jobs.ReleaseJob(releaseCtx, jobID, w.workerID); err != nil {
			slog.Warn("release interrupted ingestion job", "job_id", jobID, "error", observability.SafeError(err))
		}
		return
	}
	slog.ErrorContext(ctx, "ingestion_job_failed", "job_id", jobID, "error", observability.SafeError(cause))
	failCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.jobs.FailClaimed(failCtx, jobID, w.workerID, cause); err != nil {
		fmt.Fprintf(os.Stderr, "mark ingestion job %s failed: %v\n", jobID, err)
	}
}
