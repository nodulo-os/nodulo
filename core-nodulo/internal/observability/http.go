package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type contextKey struct{}

type requestMetadata struct {
	requestID string
	actorID   string
	method    string
	route     string
	status    int
}

// Monitor keeps bounded, process-local operational metrics. It intentionally
// stores route templates and audit metadata only, never request bodies.
type Monitor struct {
	mu                  sync.RWMutex
	started             time.Time
	inFlight            int64
	totalRequests       uint64
	statusCounts        map[string]uint64
	routes              map[string]*routeMetric
	buckets             [60]minuteBucket
	audits              []AuditEvent
	errorEvents         []ErrorEvent
	errorQueue          chan ErrorEvent
	droppedErrors       uint64
	persistenceFailures uint64
}

type routeMetric struct {
	Count             uint64  `json:"count"`
	Errors            uint64  `json:"errors"`
	TotalDurationMS   uint64  `json:"-"`
	AverageDurationMS float64 `json:"average_duration_ms"`
}
type minuteBucket struct {
	Minute   int64  `json:"minute"`
	Requests uint64 `json:"requests"`
	Errors   uint64 `json:"errors"`
}
type AuditEvent struct {
	Time        time.Time `json:"time"`
	Event       string    `json:"event"`
	RequestID   string    `json:"request_id,omitempty"`
	ActorUserID string    `json:"actor_user_id,omitempty"`
}
type ErrorEvent struct {
	ID          int64     `json:"id,omitempty"`
	Time        time.Time `json:"occurred_at"`
	Level       string    `json:"level"`
	Source      string    `json:"source"`
	Message     string    `json:"message"`
	Details     string    `json:"details,omitempty"`
	RequestID   string    `json:"request_id,omitempty"`
	ActorUserID string    `json:"actor_user_id,omitempty"`
	Method      string    `json:"method,omitempty"`
	Route       string    `json:"route,omitempty"`
	StatusCode  int       `json:"status_code,omitempty"`
}
type RouteSnapshot struct {
	Route             string  `json:"route"`
	Count             uint64  `json:"count"`
	Errors            uint64  `json:"errors"`
	AverageDurationMS float64 `json:"average_duration_ms"`
}
type Snapshot struct {
	StartedAt              time.Time         `json:"started_at"`
	UptimeSeconds          int64             `json:"uptime_seconds"`
	InFlight               int64             `json:"in_flight"`
	TotalRequests          uint64            `json:"total_requests"`
	StatusCounts           map[string]uint64 `json:"status_counts"`
	Routes                 []RouteSnapshot   `json:"routes"`
	RequestsPerMinute      []minuteBucket    `json:"requests_per_minute"`
	RecentAudit            []AuditEvent      `json:"recent_audit"`
	RecentErrors           []ErrorEvent      `json:"recent_errors"`
	DroppedErrors          uint64            `json:"dropped_errors"`
	LogPersistenceFailures uint64            `json:"log_persistence_failures"`
}

func NewMonitor(started time.Time) *Monitor {
	return &Monitor{started: started, statusCounts: make(map[string]uint64), routes: make(map[string]*routeMetric)}
}

func (m *Monitor) begin() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.inFlight++
	m.mu.Unlock()
}
func (m *Monitor) finish(route string, status int, elapsed time.Duration) {
	if m == nil {
		return
	}
	if route == "" {
		route = "(rota não identificada)"
	}
	minute := time.Now().UTC().Unix() / 60
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.inFlight > 0 {
		m.inFlight--
	}
	m.totalRequests++
	statusKey := fmt.Sprintf("%dxx", status/100)
	m.statusCounts[statusKey]++
	metric := m.routes[route]
	if metric == nil {
		metric = &routeMetric{}
		m.routes[route] = metric
	}
	metric.Count++
	metric.TotalDurationMS += uint64(elapsed.Milliseconds())
	metric.AverageDurationMS = float64(metric.TotalDurationMS) / float64(metric.Count)
	if status >= 400 {
		metric.Errors++
	}
	index := minute % int64(len(m.buckets))
	bucket := &m.buckets[index]
	if bucket.Minute != minute {
		*bucket = minuteBucket{Minute: minute}
	}
	bucket.Requests++
	if status >= 400 {
		bucket.Errors++
	}
}

func (m *Monitor) recordAudit(event AuditEvent) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.audits = append(m.audits, event)
	if len(m.audits) > 100 {
		m.audits = append([]AuditEvent(nil), m.audits[len(m.audits)-100:]...)
	}
}

// RecordError keeps an in-memory recent view and queues the event for durable
// storage. Details are diagnostics only; callers must never pass user content.
func (m *Monitor) RecordError(event ErrorEvent) {
	if m == nil {
		return
	}
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	if event.Level != "WARN" {
		event.Level = "ERROR"
	}
	event.Message = safeLogField(event.Message, 240)
	event.Details = safeLogField(event.Details, 1800)
	m.mu.Lock()
	m.errorEvents = append(m.errorEvents, event)
	if len(m.errorEvents) > 500 {
		m.errorEvents = append([]ErrorEvent(nil), m.errorEvents[len(m.errorEvents)-500:]...)
	}
	if m.errorQueue != nil {
		select {
		case m.errorQueue <- event:
		default:
			m.droppedErrors++
		}
	}
	m.mu.Unlock()
}

// StartErrorPersistence drains captured diagnostic events into durable
// storage without holding up request or model-streaming goroutines.
func (m *Monitor) StartErrorPersistence(ctx context.Context, sink func(context.Context, ErrorEvent) error) {
	if m == nil || sink == nil {
		return
	}
	m.mu.Lock()
	if m.errorQueue != nil {
		m.mu.Unlock()
		return
	}
	m.errorQueue = make(chan ErrorEvent, 1024)
	queue := m.errorQueue
	m.mu.Unlock()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-queue:
				persistCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
				if err := sink(persistCtx, event); err != nil {
					m.mu.Lock()
					m.persistenceFailures++
					m.mu.Unlock()
					fmt.Fprintf(os.Stderr, "persist observability event failed: %s\n", SafeError(err))
				}
				cancel()
			}
		}
	}()
}

func (m *Monitor) Snapshot() Snapshot {
	if m == nil {
		return Snapshot{}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := time.Now().UTC()
	snapshot := Snapshot{StartedAt: m.started, UptimeSeconds: int64(time.Since(m.started).Seconds()), InFlight: m.inFlight, TotalRequests: m.totalRequests,
		StatusCounts: make(map[string]uint64, len(m.statusCounts)), Routes: make([]RouteSnapshot, 0, len(m.routes)), RequestsPerMinute: make([]minuteBucket, 0, len(m.buckets)), RecentAudit: append([]AuditEvent(nil), m.audits...), RecentErrors: append([]ErrorEvent(nil), m.errorEvents...), DroppedErrors: m.droppedErrors, LogPersistenceFailures: m.persistenceFailures}
	for key, value := range m.statusCounts {
		snapshot.StatusCounts[key] = value
	}
	for route, metric := range m.routes {
		snapshot.Routes = append(snapshot.Routes, RouteSnapshot{Route: route, Count: metric.Count, Errors: metric.Errors, AverageDurationMS: metric.AverageDurationMS})
	}
	sort.Slice(snapshot.Routes, func(i, j int) bool { return snapshot.Routes[i].Count > snapshot.Routes[j].Count })
	if len(snapshot.Routes) > 20 {
		snapshot.Routes = snapshot.Routes[:20]
	}
	currentMinute := now.Unix() / 60
	for i := int64(59); i >= 0; i-- {
		minute := currentMinute - i
		bucket := m.buckets[minute%int64(len(m.buckets))]
		if bucket.Minute == minute {
			snapshot.RequestsPerMinute = append(snapshot.RequestsPerMinute, bucket)
		} else {
			snapshot.RequestsPerMinute = append(snapshot.RequestsPerMinute, minuteBucket{Minute: minute})
		}
	}
	for i, j := 0, len(snapshot.RecentAudit)-1; i < j; i, j = i+1, j-1 {
		snapshot.RecentAudit[i], snapshot.RecentAudit[j] = snapshot.RecentAudit[j], snapshot.RecentAudit[i]
	}
	for i, j := 0, len(snapshot.RecentErrors)-1; i < j; i, j = i+1, j-1 {
		snapshot.RecentErrors[i], snapshot.RecentErrors[j] = snapshot.RecentErrors[j], snapshot.RecentErrors[i]
	}
	return snapshot
}

var defaultMonitor *Monitor
var defaultMonitorMu sync.RWMutex

func SetDefaultMonitor(m *Monitor) {
	defaultMonitorMu.Lock()
	defaultMonitor = m
	defaultMonitorMu.Unlock()
}

var (
	urlInErrorPattern    = regexp.MustCompile(`(?i)\b(?:https?|postgres(?:ql)?|file)://[^\s"'<>]+`)
	secretInErrorPattern = regexp.MustCompile(`(?i)(password|passwd|token|secret|api[_-]?key)=([^&\s]+)`)
)

// RequestID returns the server-generated identifier for a request, when set.
func RequestID(ctx context.Context) string {
	metadata, _ := ctx.Value(contextKey{}).(*requestMetadata)
	if metadata == nil {
		return ""
	}
	return metadata.requestID
}

// SetActor associates the authenticated user with the current request log.
func SetActor(ctx context.Context, userID string) {
	metadata, _ := ctx.Value(contextKey{}).(*requestMetadata)
	if metadata != nil {
		metadata.actorID = userID
	}
}

// SetRequestRoute attaches the ServeMux route template to request diagnostics.
func SetRequestRoute(ctx context.Context, method, route string) {
	metadata, _ := ctx.Value(contextKey{}).(*requestMetadata)
	if metadata != nil {
		metadata.method = method
		metadata.route = route
	}
}

// Audit writes a structured audit event correlated with the HTTP request.
// Callers must provide metadata only; request bodies, credentials, and tokens
// should never be passed as attributes.
func Audit(ctx context.Context, event string, attributes ...any) {
	fields := make([]any, 0, len(attributes)+4)
	fields = append(fields, "event", event)
	if requestID := RequestID(ctx); requestID != "" {
		fields = append(fields, "request_id", requestID)
	}
	fields = append(fields, attributes...)
	slog.InfoContext(ctx, "audit", fields...)
	defaultMonitorMu.RLock()
	monitor := defaultMonitor
	defaultMonitorMu.RUnlock()
	if monitor != nil {
		metadata, _ := ctx.Value(contextKey{}).(*requestMetadata)
		record := AuditEvent{Time: time.Now().UTC(), Event: event}
		if metadata != nil {
			record.RequestID = metadata.requestID
			record.ActorUserID = metadata.actorID
		}
		monitor.recordAudit(record)
	}
}

// SafeError preserves useful error diagnostics while stripping URL paths,
// query parameters, URL credentials, and common inline secret assignments.
func SafeError(err error) string {
	if err == nil {
		return ""
	}
	message := urlInErrorPattern.ReplaceAllStringFunc(err.Error(), func(rawURL string) string {
		parsed, parseErr := url.Parse(strings.TrimRight(rawURL, ".,);]"))
		if parseErr != nil || parsed.Scheme == "" || parsed.Host == "" {
			return "[redacted-url]"
		}
		return parsed.Scheme + "://" + parsed.Host + "/[redacted]"
	})
	return secretInErrorPattern.ReplaceAllString(message, "$1=[redacted]")
}

// RequestLogger records one JSON log entry for every HTTP request. Query
// strings and bodies are intentionally omitted because they may contain
// credentials, personal information, or user prompts.
func RequestLogger(next http.Handler) http.Handler {
	return RequestLoggerWithMonitor(nil, next)
}

func RequestLoggerWithMonitor(monitor *Monitor, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := newRequestID()
		metadata := &requestMetadata{requestID: requestID, method: r.Method}
		ctx := context.WithValue(r.Context(), contextKey{}, metadata)
		r = r.WithContext(ctx)
		w.Header().Set("X-Request-ID", requestID)

		recorder := &responseRecorder{ResponseWriter: w}
		started := time.Now()
		monitor.begin()
		next.ServeHTTP(recorder, r)
		if recorder.status == 0 {
			recorder.status = http.StatusOK
		}
		route := r.Pattern
		if metadata.route != "" {
			route = metadata.route
		}
		if route == "" {
			route = scrubUUIDPath(r.URL.Path)
			if route == "" {
				route = "(rota não identificada)"
			}
		}
		metadata.route = route
		metadata.status = recorder.status
		monitor.finish(route, recorder.status, time.Since(started))
		if recorder.status >= http.StatusBadRequest {
			level := "WARN"
			message := "A API recusou a solicitação. Consulte o status e o request ID para identificar a causa."
			if recorder.status >= http.StatusInternalServerError {
				level = "ERROR"
				message = "A API não conseguiu concluir a solicitação. Consulte os logs correlacionados pelo request ID."
			}
			monitor.RecordError(ErrorEvent{Time: time.Now().UTC(), Level: level, Source: "http", Message: message,
				Details:   fmt.Sprintf("HTTP %d · %s %s · duração %d ms", recorder.status, r.Method, route, time.Since(started).Milliseconds()),
				RequestID: requestID, ActorUserID: metadata.actorID, Method: r.Method, Route: route, StatusCode: recorder.status})
		}

		attributes := []any{
			"request_id", requestID,
			"method", r.Method,
			"path", route,
			"status", recorder.status,
			"bytes_written", recorder.bytesWritten,
			"duration_ms", time.Since(started).Milliseconds(),
			"remote_addr", remoteHost(r.RemoteAddr),
		}
		if agent := r.UserAgent(); agent != "" {
			attributes = append(attributes, "user_agent", agent)
		}
		if metadata.actorID != "" {
			attributes = append(attributes, "actor_user_id", metadata.actorID)
		}
		if recorder.status >= http.StatusInternalServerError {
			slog.ErrorContext(ctx, "http_request", attributes...)
		} else if recorder.status >= http.StatusBadRequest {
			slog.WarnContext(ctx, "http_request", attributes...)
		} else {
			slog.InfoContext(ctx, "http_request", attributes...)
		}
	})
}

type capturingSlogHandler struct {
	next    slog.Handler
	monitor *Monitor
}

func NewCapturingSlogHandler(next slog.Handler, monitor *Monitor) slog.Handler {
	return &capturingSlogHandler{next: next, monitor: monitor}
}

func (h *capturingSlogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}
func (h *capturingSlogHandler) Handle(ctx context.Context, record slog.Record) error {
	if h.monitor != nil && record.Level >= slog.LevelWarn && record.Message != "http_request" {
		event := ErrorEvent{Time: record.Time.UTC(), Level: "WARN", Source: "application", Message: record.Message}
		if record.Level >= slog.LevelError {
			event.Level = "ERROR"
		}
		if metadata, _ := ctx.Value(contextKey{}).(*requestMetadata); metadata != nil {
			event.RequestID, event.ActorUserID, event.Method, event.Route, event.StatusCode = metadata.requestID, metadata.actorID, metadata.method, metadata.route, metadata.status
		}
		var details []string
		record.Attrs(func(attribute slog.Attr) bool {
			key := strings.ToLower(attribute.Key)
			switch key {
			case "request_id":
				if event.RequestID == "" {
					event.RequestID = slogValueString(attribute.Value)
				}
			case "actor_user_id", "user_id":
				if event.ActorUserID == "" {
					event.ActorUserID = slogValueString(attribute.Value)
				}
			case "method":
				if event.Method == "" {
					event.Method = slogValueString(attribute.Value)
				}
			case "path", "route":
				if event.Route == "" {
					event.Route = slogValueString(attribute.Value)
				}
			case "status", "status_code":
				if event.StatusCode == 0 {
					_, _ = fmt.Sscan(slogValueString(attribute.Value), &event.StatusCode)
				}
			case "error", "reason", "operation", "model", "job_id", "conversation_id", "attempt", "max_attempts", "elapsed_ms", "response_characters", "attachment_count", "rag_sources":
				details = append(details, key+"="+slogValueString(attribute.Value))
			}
			return true
		})
		event.Details = strings.Join(details, " · ")
		h.monitor.RecordError(event)
	}
	return h.next.Handle(ctx, record)
}
func (h *capturingSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &capturingSlogHandler{next: h.next.WithAttrs(attrs), monitor: h.monitor}
}
func (h *capturingSlogHandler) WithGroup(name string) slog.Handler {
	return &capturingSlogHandler{next: h.next.WithGroup(name), monitor: h.monitor}
}

func slogValueString(value slog.Value) string {
	value = value.Resolve()
	if value.Kind() == slog.KindString {
		return value.String()
	}
	return fmt.Sprint(value.Any())
}

type logCaptureWriter struct {
	monitor *Monitor
	output  io.Writer
}

func NewLogCaptureWriter(monitor *Monitor, output io.Writer) io.Writer {
	return &logCaptureWriter{monitor: monitor, output: output}
}
func (w *logCaptureWriter) Write(data []byte) (int, error) {
	line := strings.TrimSpace(string(data))
	if line != "" && w.monitor != nil {
		line = strings.TrimSpace(strings.TrimPrefix(line, time.Now().Format("2006/01/02 15:04:05 ")))
		w.monitor.RecordError(ErrorEvent{Time: time.Now().UTC(), Level: "ERROR", Source: "legacy_log", Message: "Registro do servidor", Details: line})
	}
	if w.output == nil {
		return len(data), nil
	}
	return w.output.Write(data)
}

func safeLogField(value string, limit int) string {
	value = SafeError(errorsNew(value))
	value = strings.TrimSpace(value)
	if len(value) > limit {
		value = value[:limit] + "…"
	}
	return value
}

func errorsNew(value string) error { return fmt.Errorf("%s", value) }

var uuidPathPattern = regexp.MustCompile(`(?i)/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

func scrubUUIDPath(path string) string { return uuidPathPattern.ReplaceAllString(path, "/{id}") }

type responseRecorder struct {
	http.ResponseWriter
	status       int
	bytesWritten int64
}

func (w *responseRecorder) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseRecorder) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	written, err := w.ResponseWriter.Write(body)
	w.bytesWritten += int64(written)
	return written, err
}

func (w *responseRecorder) Flush() {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *responseRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func remoteHost(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return address
	}
	return host
}

func newRequestID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return strings.ReplaceAll(time.Now().UTC().Format("20060102T150405.000000000"), ".", "-")
	}
	return hex.EncodeToString(id[:])
}
