package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"core-nodulo/internal/config"
	"core-nodulo/internal/handlers"
	"core-nodulo/internal/middleware"
	"core-nodulo/internal/observability"
	"core-nodulo/internal/repository"
	"core-nodulo/internal/services"
	"core-nodulo/pkg/db"
	"core-nodulo/pkg/ollama"
	"core-nodulo/pkg/search"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	inferenceURLFromProcess := os.Getenv("INFERENCE_BASE_URL")
	legacyInferenceURLFromProcess := os.Getenv("OLLAMA_BASE_URL")
	generationModelFromProcess := os.Getenv("GENERATION_MODEL")
	legacyGenerationModelFromProcess := os.Getenv("LLM_MODEL")
	inferenceURLSetByProcess := inferenceURLFromProcess != "" || legacyInferenceURLFromProcess != ""
	generationModelSetByProcess := generationModelFromProcess != "" || legacyGenerationModelFromProcess != ""
	dotenvPath, err := config.LoadDotEnv()
	if err != nil {
		log.Fatalf("carregar arquivo .env: %v", err)
	}
	if dotenvPath != "" {
		if absolutePath, absErr := filepath.Abs(dotenvPath); absErr == nil {
			dotenvPath = absolutePath
		}
	}
	frontendOrigin := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	port := os.Getenv("PORT")
	if port == "" {
		log.Fatal("PORT é obrigatória")
	}
	if frontendOrigin == "" {
		log.Fatal("FRONTEND_ORIGIN é obrigatória")
	}
	if err := middleware.ValidateFrontendOrigin(frontendOrigin); err != nil {
		log.Fatal(err)
	}
	inferenceBaseURL := inferenceURLFromProcess
	if inferenceBaseURL == "" {
		inferenceBaseURL = legacyInferenceURLFromProcess
	}
	if inferenceBaseURL == "" {
		inferenceBaseURL = os.Getenv("INFERENCE_BASE_URL")
	}
	if inferenceBaseURL == "" {
		inferenceBaseURL = os.Getenv("OLLAMA_BASE_URL")
	}
	if inferenceBaseURL == "" {
		log.Fatal("INFERENCE_BASE_URL é obrigatória")
	}
	generationModel := generationModelFromProcess
	if generationModel == "" {
		generationModel = legacyGenerationModelFromProcess
	}
	if generationModel == "" {
		generationModel = os.Getenv("GENERATION_MODEL")
	}
	if generationModel == "" {
		generationModel = os.Getenv("LLM_MODEL")
	}
	if generationModel == "" {
		log.Fatal("GENERATION_MODEL é obrigatória")
	}
	embeddingModel := os.Getenv("EMBEDDING_MODEL")
	if embeddingModel == "" {
		log.Fatal("EMBEDDING_MODEL é obrigatória")
	}
	visionModel := strings.TrimSpace(os.Getenv("VISION_MODEL"))
	if visionModel == "" {
		visionModel = strings.TrimSpace(os.Getenv("LLM_VISION_MODEL"))
	}
	jwtSecret := os.Getenv("JWT_SECRET")
	if len([]byte(jwtSecret)) < 32 {
		log.Fatal("JWT_SECRET é obrigatória e deve ter pelo menos 32 bytes. Gere uma com: openssl rand -base64 48")
	}
	trustedProxies, err := middleware.ParseTrustedProxyCIDRs(os.Getenv("TRUSTED_PROXY_CIDRS"))
	if err != nil {
		log.Fatal(err)
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://nodulo:nodulopassword@localhost:5432/nodulo?sslmode=disable"
	}
	slog.Info("configuration loaded",
		"dotenv_path", dotenvPath,
		"inference_url_config_source", configSource(inferenceURLSetByProcess, dotenvPath),
		"inference_endpoint", safeEndpoint(inferenceBaseURL),
		"generation_model_config_source", configSource(generationModelSetByProcess, dotenvPath),
		"generation_model", generationModel,
		"embedding_model", embeddingModel,
		"vision_model_configured", visionModel != "",
		"port", port,
	)

	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelStartup()
	pool, err := db.NewPostgresPool(startupCtx, databaseURL)
	if err != nil {
		log.Fatalf("Conexão com PostgreSQL falhou: %v", err)
	}
	defer pool.Close()
	systemLogRepo := repository.NewSystemLogRepository(pool)

	ollamaClient := ollama.NewClient(inferenceBaseURL)
	monitor := observability.NewMonitor(time.Now())
	observability.SetDefaultMonitor(monitor)
	slog.SetDefault(slog.New(observability.NewCapturingSlogHandler(
		slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}), monitor,
	)))
	log.SetOutput(observability.NewLogCaptureWriter(monitor, os.Stdout))
	logPersistenceCtx, cancelLogPersistence := context.WithCancel(context.Background())
	defer cancelLogPersistence()
	monitor.StartErrorPersistence(logPersistenceCtx, systemLogRepo.InsertErrorLog)
	chatRepo := repository.NewChatRepository(pool)
	userMemoryRepo := repository.NewUserMemoryRepository(pool)
	memoryService := services.NewMemoryService(ollamaClient, userMemoryRepo, generationModel, embeddingModel)
	memoryHandler := handlers.NewMemoryHandler(userMemoryRepo)
	chatUploadSettingsRepo := repository.NewChatUploadSettingsRepository(pool)
	userRepo := repository.NewUserRepository(pool)
	registrationRepo := repository.NewRegistrationRepository(pool)
	institutionalSettingsRepo := repository.NewInstitutionalSettingsRepository(pool)
	webSearchSettingsRepo := repository.NewWebSearchSettingsRepository(pool)
	adminRepo := repository.NewAdminRepository(pool)
	ingestionRepo := repository.NewIngestionRepository(pool)
	ragService := services.NewRAGService(ollamaClient, chatRepo, embeddingModel)
	ragService.SetVisionModel(visionModel)
	ragService.SetWebSearchConfiguration(webSearchSettingsRepo, map[string]services.WebSearchClient{
		repository.WebSearchProviderDuckDuckGo: search.NewDuckDuckGoClient(),
	})
	worker := services.NewJobWorker(ingestionRepo, ragService, 2, 64)
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	defer cancelWorker()
	worker.Start(workerCtx)
	chatHandler := handlers.NewChatHandler(ollamaClient, chatRepo, chatUploadSettingsRepo, ragService, generationModel)
	chatHandler.SetMemoryService(memoryService)
	chatHandler.SetTitleGenerator(services.NewTitleGenerator(ollamaClient, chatRepo, generationModel))
	chatUploadSettingsHandler := handlers.NewChatUploadSettingsHandler(chatUploadSettingsRepo)
	registrationNotifier := services.NewSMTPRegistrationNotifier(
		os.Getenv("SMTP_HOST"), os.Getenv("SMTP_PORT"), os.Getenv("SMTP_USERNAME"),
		os.Getenv("SMTP_PASSWORD"), os.Getenv("SMTP_FROM"), os.Getenv("APP_PUBLIC_URL"),
	)
	adminHandler := handlers.NewAdminHandler(adminRepo, userRepo, ingestionRepo, worker, ollamaClient, ragService, registrationRepo, registrationNotifier)
	observabilityHandler := handlers.NewObservabilityHandler(monitor, pool, ollamaClient, systemLogRepo)
	institutionalSettingsHandler := handlers.NewInstitutionalSettingsHandler(institutionalSettingsRepo)
	webSearchSettingsHandler := handlers.NewWebSearchSettingsHandler(webSearchSettingsRepo)
	authService := services.NewAuthService(userRepo, registrationRepo, jwtSecret)
	authHandler := handlers.NewAuthHandler(authService, userRepo, registrationNotifier)
	requireAuth := middleware.RequireAuth(jwtSecret)
	requireCurrentAdmin := middleware.RequireCurrentAdmin(userRepo)
	adminProtected := func(handler http.Handler) http.Handler {
		return requireAuth(requireCurrentAdmin(middleware.RequireAdmin(handler)))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/register", authHandler.HandleRegister)
	mux.HandleFunc("POST /api/auth/login", authHandler.HandleLogin)
	mux.Handle("GET /api/auth/me", requireAuth(http.HandlerFunc(authHandler.HandleMe)))
	mux.Handle("GET /api/memory", requireAuth(http.HandlerFunc(memoryHandler.HandleGet)))
	mux.Handle("PATCH /api/memory/settings", requireAuth(http.HandlerFunc(memoryHandler.HandleUpdateSettings)))
	mux.Handle("DELETE /api/memory", requireAuth(http.HandlerFunc(memoryHandler.HandleDeleteAll)))
	mux.Handle("DELETE /api/memory/{id}", requireAuth(http.HandlerFunc(memoryHandler.HandleDeleteFact)))
	mux.Handle("POST /api/chat", requireAuth(http.HandlerFunc(chatHandler.HandleChat)))
	mux.Handle("GET /api/chat/upload-settings", requireAuth(http.HandlerFunc(chatUploadSettingsHandler.HandleGet)))
	mux.Handle("POST /api/documents/ingest", requireAuth(requireCurrentAdmin(middleware.RequireAdmin(http.HandlerFunc(chatHandler.HandleIngestDocument)))))
	mux.Handle("GET /api/conversations", requireAuth(http.HandlerFunc(chatHandler.HandleListConversations)))
	mux.Handle("GET /api/conversations/archived", requireAuth(http.HandlerFunc(chatHandler.HandleListArchivedConversations)))
	mux.Handle("GET /api/conversations/", requireAuth(http.HandlerFunc(chatHandler.HandleListConversations)))
	mux.Handle("GET /api/conversations/{id}/messages", requireAuth(http.HandlerFunc(chatHandler.HandleGetMessages)))
	mux.Handle("PATCH /api/conversations/{id}/archive", requireAuth(http.HandlerFunc(chatHandler.HandleArchiveConversation)))
	mux.Handle("DELETE /api/conversations/{id}", requireAuth(http.HandlerFunc(chatHandler.HandleDeleteConversation)))
	mux.Handle("GET /api/conversations/{id}", requireAuth(http.HandlerFunc(chatHandler.HandleGetMessages)))
	mux.Handle("PATCH /api/conversations/{id}/folder", requireAuth(http.HandlerFunc(chatHandler.HandleMoveConversation)))
	mux.Handle("GET /api/conversation-folders", requireAuth(http.HandlerFunc(chatHandler.HandleListConversationFolders)))
	mux.Handle("POST /api/conversation-folders", requireAuth(http.HandlerFunc(chatHandler.HandleCreateConversationFolder)))
	mux.Handle("PATCH /api/conversation-folders/{id}", requireAuth(http.HandlerFunc(chatHandler.HandleRenameConversationFolder)))
	mux.Handle("DELETE /api/conversation-folders/{id}", requireAuth(http.HandlerFunc(chatHandler.HandleDeleteConversationFolder)))
	mux.Handle("GET /api/admin/stats", adminProtected(http.HandlerFunc(adminHandler.HandleGetStats)))
	mux.Handle("GET /api/admin/observability", adminProtected(http.HandlerFunc(observabilityHandler.HandleGet)))
	mux.Handle("GET /api/admin/observability/errors", adminProtected(http.HandlerFunc(observabilityHandler.HandleListErrors)))
	mux.Handle("GET /api/admin/chat-upload-settings", adminProtected(http.HandlerFunc(chatUploadSettingsHandler.HandleGet)))
	mux.Handle("PATCH /api/admin/chat-upload-settings", adminProtected(http.HandlerFunc(chatUploadSettingsHandler.HandleUpdate)))
	mux.Handle("GET /api/admin/users", adminProtected(http.HandlerFunc(adminHandler.HandleListUsers)))
	mux.Handle("GET /api/admin/registration/settings", adminProtected(http.HandlerFunc(adminHandler.HandleGetRegistrationSettings)))
	mux.HandleFunc("GET /api/site-settings", institutionalSettingsHandler.HandleGetPublic)
	mux.Handle("GET /api/admin/site-settings", adminProtected(http.HandlerFunc(institutionalSettingsHandler.HandleGetAdmin)))
	mux.Handle("PATCH /api/admin/site-settings", adminProtected(http.HandlerFunc(institutionalSettingsHandler.HandleUpdate)))
	mux.Handle("GET /api/admin/web-search-settings", adminProtected(http.HandlerFunc(webSearchSettingsHandler.HandleGet)))
	mux.Handle("PATCH /api/admin/web-search-settings", adminProtected(http.HandlerFunc(webSearchSettingsHandler.HandleUpdate)))
	mux.Handle("PATCH /api/admin/registration/settings", adminProtected(http.HandlerFunc(adminHandler.HandleUpdateRegistrationSettings)))
	mux.Handle("GET /api/admin/registration/allowlist", adminProtected(http.HandlerFunc(adminHandler.HandleListAuthorizedEmails)))
	mux.Handle("POST /api/admin/registration/allowlist", adminProtected(http.HandlerFunc(adminHandler.HandleAddAuthorizedEmails)))
	mux.Handle("DELETE /api/admin/registration/allowlist", adminProtected(http.HandlerFunc(adminHandler.HandleDeleteAuthorizedEmail)))
	mux.Handle("POST /api/admin/users/{id}/review", adminProtected(http.HandlerFunc(adminHandler.HandleReviewRegistration)))
	mux.Handle("POST /api/admin/users/{id}/notification", adminProtected(http.HandlerFunc(adminHandler.HandleResendRegistrationNotification)))
	mux.Handle("PATCH /api/admin/users/{id}", adminProtected(http.HandlerFunc(adminHandler.HandleUpdateUserRole)))
	mux.Handle("DELETE /api/admin/users/{id}", adminProtected(http.HandlerFunc(adminHandler.HandleDeleteUser)))
	mux.Handle("GET /api/admin/models", adminProtected(http.HandlerFunc(adminHandler.HandleListModels)))
	mux.Handle("POST /api/admin/models/pull", adminProtected(http.HandlerFunc(adminHandler.HandlePullModel)))
	mux.Handle("DELETE /api/admin/models", adminProtected(http.HandlerFunc(adminHandler.HandleDeleteModel)))
	mux.Handle("GET /api/admin/documents", adminProtected(http.HandlerFunc(adminHandler.HandleListDocuments)))
	mux.Handle("DELETE /api/admin/documents", adminProtected(http.HandlerFunc(adminHandler.HandleDeleteDocument)))
	// Upload único e em lote usam a mesma fila para evitar manter o request aberto
	// durante extração, embeddings e persistência.
	mux.Handle("POST /api/admin/documents/upload", adminProtected(http.HandlerFunc(adminHandler.HandleUploadBatch)))
	mux.Handle("POST /api/admin/documents/upload-batch", adminProtected(http.HandlerFunc(adminHandler.HandleUploadBatch)))
	mux.Handle("POST /api/admin/documents/scrape", adminProtected(http.HandlerFunc(adminHandler.HandleScrapeURL)))
	mux.Handle("POST /api/admin/documents/scrape-url", adminProtected(http.HandlerFunc(adminHandler.HandleScrapeURL)))
	mux.Handle("GET /api/admin/jobs", adminProtected(http.HandlerFunc(adminHandler.HandleListJobs)))
	mux.Handle("GET /api/admin/jobs/stream", adminProtected(http.HandlerFunc(adminHandler.HandleStreamJobs)))
	mux.Handle("POST /api/admin/jobs/{id}/retry", adminProtected(http.HandlerFunc(adminHandler.HandleRetryJob)))
	mux.Handle("/api/admin/", adminProtected(http.HandlerFunc(adminHandler.HandleNotFound)))
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           observability.RequestLoggerWithMonitor(monitor, middleware.SecurityHeaders(middleware.CORS(frontendOrigin, middleware.AuthRateLimiter(trustedProxies, mux)))),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Minute,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
		// WriteTimeout remains zero because /api/chat streams responses.
	}

	slog.Info("api listening", "address", server.Addr, "inference_endpoint", safeEndpoint(inferenceBaseURL), "generation_model", generationModel, "embedding_model", embeddingModel)
	shutdownSignals, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.ListenAndServe() }()
	var serverErr error
	select {
	case serverErr = <-serverErrors:
		if serverErr == http.ErrServerClosed {
			serverErr = nil
		}
	case <-shutdownSignals.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTP server graceful shutdown: %v", err)
			_ = server.Close()
		}
		cancel()
		<-serverErrors
	}
	workerShutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 30*time.Second)
	if err := worker.Shutdown(workerShutdownCtx); err != nil {
		log.Printf("ingestion worker shutdown: %v", err)
	}
	cancelShutdown()
	if serverErr != nil {
		log.Fatal(serverErr)
	}
}

func safeEndpoint(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "invalid"
	}
	return parsed.Scheme + "://" + parsed.Host
}

func configSource(setByProcess bool, dotenvPath string) string {
	if setByProcess {
		return "process_environment (takes precedence over .env)"
	}
	if dotenvPath != "" {
		return "dotenv"
	}
	return "not_set"
}
