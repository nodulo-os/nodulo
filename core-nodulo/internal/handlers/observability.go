package handlers

import (
	"context"
	"net/http"
	"runtime"
	"strconv"
	"time"

	"core-nodulo/internal/observability"
	"core-nodulo/internal/repository"
	"core-nodulo/pkg/ollama"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ObservabilityHandler struct {
	Monitor   *observability.Monitor
	Pool      *pgxpool.Pool
	Inference *ollama.Client
	ErrorLogs *repository.SystemLogRepository
}

func NewObservabilityHandler(monitor *observability.Monitor, pool *pgxpool.Pool, inference *ollama.Client, errorLogs *repository.SystemLogRepository) *ObservabilityHandler {
	return &ObservabilityHandler{Monitor: monitor, Pool: pool, Inference: inference, ErrorLogs: errorLogs}
}

func (h *ObservabilityHandler) HandleListErrors(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	beforeID, _ := strconv.ParseInt(r.URL.Query().Get("before_id"), 10, 64)
	page, err := h.ErrorLogs.ListErrorLogs(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("level"), beforeID, limit)
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "Não foi possível consultar o histórico de erros e logs.")
		return
	}
	writeJSON(w, http.StatusOK, page)
}

type dependencyHealth struct {
	Status    string    `json:"status"`
	LatencyMS int64     `json:"latency_ms,omitempty"`
	Models    int       `json:"available_models,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

type processHealth struct {
	MemoryAllocBytes uint64 `json:"memory_alloc_bytes"`
	MemorySysBytes   uint64 `json:"memory_sys_bytes"`
	Goroutines       int    `json:"goroutines"`
	GCCount          uint32 `json:"gc_count"`
}

type observabilityResponse struct {
	Metrics      observability.Snapshot      `json:"metrics"`
	Process      processHealth               `json:"process"`
	Dependencies map[string]dependencyHealth `json:"dependencies"`
}

func (h *ObservabilityHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
	response := observabilityResponse{Metrics: h.Monitor.Snapshot(), Dependencies: make(map[string]dependencyHealth, 2)}
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	response.Process = processHealth{MemoryAllocBytes: memory.Alloc, MemorySysBytes: memory.Sys, Goroutines: runtime.NumGoroutine(), GCCount: memory.NumGC}

	checks, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	type dependencyResult struct {
		name   string
		health dependencyHealth
	}
	results := make(chan dependencyResult, 2)
	go func() {
		results <- dependencyResult{name: "database", health: checkDependency(checks, func(ctx context.Context) error { return h.Pool.Ping(ctx) })}
	}()
	go func() { results <- dependencyResult{name: "inference", health: checkInference(checks, h.Inference)} }()
	for range 2 {
		result := <-results
		response.Dependencies[result.name] = result.health
	}
	writeJSON(w, http.StatusOK, response)
}

func checkDependency(ctx context.Context, check func(context.Context) error) dependencyHealth {
	started := time.Now()
	err := check(ctx)
	health := dependencyHealth{Status: "ok", LatencyMS: time.Since(started).Milliseconds(), CheckedAt: time.Now().UTC()}
	if err != nil {
		health.Status = "unavailable"
	}
	return health
}

func checkInference(ctx context.Context, client *ollama.Client) dependencyHealth {
	started := time.Now()
	models, err := client.ListLocalModels(ctx)
	health := dependencyHealth{Status: "ok", LatencyMS: time.Since(started).Milliseconds(), CheckedAt: time.Now().UTC()}
	if err != nil {
		health.Status = "unavailable"
		return health
	}
	health.Models = len(models)
	return health
}
