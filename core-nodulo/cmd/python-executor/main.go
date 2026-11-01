package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"core-nodulo/pkg/sandbox"
)

const maxRequestBytes = 64 << 10

type runRequest struct {
	Script string `json:"script"`
}

type runResponse struct {
	Output string `json:"output,omitempty"`
	Image  string `json:"image_base64,omitempty"`
	Error  string `json:"error,omitempty"`
}

type executor struct {
	token   string
	image   string
	permits chan struct{}
}

func main() {
	token := strings.TrimSpace(os.Getenv("PYTHON_EXECUTOR_TOKEN"))
	if len(token) < 32 {
		slog.Error("PYTHON_EXECUTOR_TOKEN must contain at least 32 characters")
		os.Exit(1)
	}
	image := strings.TrimSpace(os.Getenv("NODULO_PYTHON_SANDBOX_IMAGE"))
	if image == "" {
		image = "nodulo-python-sandbox:1.0"
	}
	_ = os.Setenv("NODULO_PYTHON_SANDBOX_IMAGE", image)
	concurrency := 2
	if raw := strings.TrimSpace(os.Getenv("PYTHON_EXECUTOR_CONCURRENCY")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 8 {
			slog.Error("PYTHON_EXECUTOR_CONCURRENCY must be an integer from 1 to 8")
			os.Exit(1)
		}
		concurrency = value
	}
	address := strings.TrimSpace(os.Getenv("PYTHON_EXECUTOR_ADDR"))
	if address == "" {
		address = ":8080"
	}

	service := &executor{token: token, image: image, permits: make(chan struct{}, concurrency)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", service.handleHealth)
	mux.HandleFunc("POST /v1/run", service.handleRun)
	server := &http.Server{
		Addr: address, Handler: mux, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 40 * time.Second,
		IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10,
	}

	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("Python sandbox broker listening", "address", address, "sandbox_image", image, "max_concurrency", concurrency)
		serverErrors <- server.ListenAndServe()
	}()
	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Python sandbox broker stopped", "error", err)
			os.Exit(1)
		}
	case <-signals.Done():
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			slog.Error("Python sandbox broker shutdown failed", "error", err)
			_ = server.Close()
		}
	}
}

func (e *executor) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if _, err := exec.LookPath("docker"); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, runResponse{Error: "Docker CLI indisponível no executor."})
		return
	}
	command := exec.CommandContext(ctx, "docker", "image", "inspect", e.image)
	if err := command.Run(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, runResponse{Error: "Daemon Docker ou imagem do sandbox indisponível."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (e *executor) handleRun(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.ErrorContext(r.Context(), "Python executor handler panicked")
			writeJSON(w, http.StatusInternalServerError, runResponse{Error: "Falha interna no executor Python."})
		}
	}()
	if !e.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, runResponse{Error: "Credencial do executor inválida."})
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeJSON(w, http.StatusUnsupportedMediaType, runResponse{Error: "Envie a solicitação como application/json."})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var payload runRequest
	if err := decoder.Decode(&payload); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeJSON(w, http.StatusRequestEntityTooLarge, runResponse{Error: "O código enviado excede 64 KiB."})
		} else {
			writeJSON(w, http.StatusBadRequest, runResponse{Error: "Corpo JSON inválido."})
		}
		return
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, runResponse{Error: "Envie apenas um objeto JSON."})
		return
	}
	if strings.TrimSpace(payload.Script) == "" {
		writeJSON(w, http.StatusBadRequest, runResponse{Error: "O código Python está vazio."})
		return
	}
	select {
	case e.permits <- struct{}{}:
		defer func() { <-e.permits }()
	default:
		writeJSON(w, http.StatusServiceUnavailable, runResponse{Error: "O executor Python está ocupado. Tente novamente em instantes."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	output, image, runErr := sandbox.RunPythonLocal(ctx, payload.Script)
	if runErr != nil {
		status := http.StatusUnprocessableEntity
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		}
		writeJSON(w, status, runResponse{Output: output, Error: safeError(runErr)})
		slog.WarnContext(r.Context(), "Python sandbox execution failed", "elapsed_ms", time.Since(started).Milliseconds())
		return
	}
	writeJSON(w, http.StatusOK, runResponse{Output: output, Image: image})
	slog.InfoContext(r.Context(), "Python sandbox execution completed", "elapsed_ms", time.Since(started).Milliseconds(), "generated_image", image != "")
}

func (e *executor) authorized(r *http.Request) bool {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return false
	}
	provided := []byte(parts[1])
	expected := []byte(e.token)
	if len(provided) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare(provided, expected) == 1
}

func safeError(err error) string {
	if err == nil {
		return "execução Python falhou"
	}
	message := strings.TrimSpace(err.Error())
	if len(message) > 2000 {
		message = message[:2000]
	}
	return message
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		// The connection may already be gone; response encoding has no recovery path.
		fmt.Fprintln(os.Stderr, "encode Python executor response:", err)
	}
}
