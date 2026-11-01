package handlers

import (
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"core-nodulo/internal/middleware"
	"core-nodulo/internal/observability"
	"core-nodulo/internal/repository"
	"core-nodulo/internal/services"
	"core-nodulo/pkg/extractor"
	"core-nodulo/pkg/ollama"
)

const (
	maxAdminJSONBytes      = 1 << 20
	maxBatchFileBytes      = 200 << 20
	maxBatchTotalFileBytes = 200 << 20
	maxBatchBodyBytes      = 256 << 20
	maxBatchFiles          = 20
)

type AdminHandler struct {
	Repository    *repository.AdminRepository
	Users         *repository.UserRepository
	Jobs          *repository.IngestionRepository
	JobWorker     *services.JobWorker
	OllamaClient  *ollama.Client
	RAG           *services.RAGService
	Registrations *repository.RegistrationRepository
	Notifier      services.RegistrationNotifier
}

type modelRequest struct {
	Model string `json:"model"`
}

type deleteDocumentRequest struct {
	SourceReference string `json:"source_reference"`
}

func NewAdminHandler(repo *repository.AdminRepository, users *repository.UserRepository, jobs *repository.IngestionRepository, worker *services.JobWorker, client *ollama.Client, rag *services.RAGService, registrations *repository.RegistrationRepository, notifier services.RegistrationNotifier) *AdminHandler {
	return &AdminHandler{Repository: repo, Users: users, Jobs: jobs, JobWorker: worker, OllamaClient: client, RAG: rag, Registrations: registrations, Notifier: notifier}
}

type registrationSettingsRequest struct {
	Mode           string   `json:"mode"`
	AllowedDomains []string `json:"allowed_domains"`
}

type authorizedEmailsRequest struct {
	Emails []string `json:"emails"`
	Note   string   `json:"note"`
}

type authorizedEmailDeleteRequest struct {
	Email string `json:"email"`
}

type registrationReviewRequest struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

var institutionalDomainPattern = regexp.MustCompile(`^(?i)(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

func (h *AdminHandler) HandleGetRegistrationSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.Registrations.GetSettings(r.Context())
	if err != nil {
		log.Printf("load registration settings: %s", observability.SafeError(err))
		writeAdminError(w, http.StatusInternalServerError, "Não foi possível consultar as regras de cadastro.")
		return
	}
	emails, err := h.Registrations.ListAuthorizedEmails(r.Context())
	if err != nil {
		log.Printf("list registration allowlist: %s", observability.SafeError(err))
		writeAdminError(w, http.StatusInternalServerError, "Não foi possível consultar a lista autorizada.")
		return
	}
	notificationConfigured := false
	if notifier, ok := h.Notifier.(interface{ Configured() bool }); ok {
		notificationConfigured = notifier.Configured()
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings, "authorized_emails": emails, "notification_configured": notificationConfigured})
}

func (h *AdminHandler) HandleListAuthorizedEmails(w http.ResponseWriter, r *http.Request) {
	items, err := h.Registrations.ListAuthorizedEmails(r.Context())
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "Não foi possível consultar a lista autorizada.")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *AdminHandler) HandleUpdateRegistrationSettings(w http.ResponseWriter, r *http.Request) {
	var payload registrationSettingsRequest
	if err := decodeAdminJSON(w, r, &payload); err != nil {
		writeAdminError(w, http.StatusBadRequest, "Payload JSON inválido.")
		return
	}
	payload.Mode = strings.TrimSpace(payload.Mode)
	if payload.Mode != repository.RegistrationModeAutomatic && payload.Mode != repository.RegistrationModeApproval {
		writeAdminError(w, http.StatusBadRequest, "mode deve ser automatic ou approval.")
		return
	}
	domains := make([]string, 0, len(payload.AllowedDomains))
	seen := make(map[string]struct{}, len(payload.AllowedDomains))
	for _, rawDomain := range payload.AllowedDomains {
		domain := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(rawDomain, "@")))
		if domain == "" {
			continue
		}
		if len(domain) > 253 || net.ParseIP(domain) != nil || !institutionalDomainPattern.MatchString(domain) {
			writeAdminError(w, http.StatusBadRequest, "Cada domínio deve ser um domínio DNS válido, como universidade.edu.br.")
			return
		}
		if _, exists := seen[domain]; !exists {
			seen[domain] = struct{}{}
			domains = append(domains, domain)
		}
	}
	if payload.Mode == repository.RegistrationModeAutomatic && len(domains) == 0 {
		emails, err := h.Registrations.ListAuthorizedEmails(r.Context())
		if err != nil {
			writeAdminError(w, http.StatusInternalServerError, "Não foi possível verificar a lista de e-mails autorizados.")
			return
		}
		if len(emails) == 0 {
			writeAdminError(w, http.StatusBadRequest, "O modo automático exige ao menos um domínio institucional ou e-mail autorizado.")
			return
		}
	}
	actor, _ := middleware.UserFromContext(r.Context())
	settings, err := h.Registrations.UpdateSettings(r.Context(), payload.Mode, domains, actor.ID)
	if err != nil {
		log.Printf("update registration settings: %s", observability.SafeError(err))
		writeAdminError(w, http.StatusInternalServerError, "Não foi possível salvar as regras de cadastro.")
		return
	}
	observability.Audit(r.Context(), "admin.registration.settings_updated", "actor_user_id", actor.ID, "mode", settings.Mode, "allowed_domains_count", len(settings.AllowedDomains))
	writeJSON(w, http.StatusOK, settings)
}

func (h *AdminHandler) HandleAddAuthorizedEmails(w http.ResponseWriter, r *http.Request) {
	var payload authorizedEmailsRequest
	if err := decodeAdminJSON(w, r, &payload); err != nil {
		writeAdminError(w, http.StatusBadRequest, "Payload JSON inválido.")
		return
	}
	if len(payload.Emails) == 0 || len(payload.Emails) > 1000 {
		writeAdminError(w, http.StatusBadRequest, "Envie entre 1 e 1000 e-mails.")
		return
	}
	if len(payload.Note) > 500 {
		writeAdminError(w, http.StatusBadRequest, "A nota administrativa deve ter até 500 caracteres.")
		return
	}
	emails := make([]string, 0, len(payload.Emails))
	seen := make(map[string]struct{}, len(payload.Emails))
	for _, rawEmail := range payload.Emails {
		email := strings.ToLower(strings.TrimSpace(rawEmail))
		parsed, err := mail.ParseAddress(email)
		if err != nil || parsed.Address != email || len(email) > 254 {
			writeAdminError(w, http.StatusBadRequest, "A lista contém um endereço de e-mail inválido.")
			return
		}
		if _, exists := seen[email]; !exists {
			seen[email] = struct{}{}
			emails = append(emails, email)
		}
	}
	actor, _ := middleware.UserFromContext(r.Context())
	count, err := h.Registrations.AddAuthorizedEmails(r.Context(), emails, strings.TrimSpace(payload.Note), actor.ID)
	if err != nil {
		log.Printf("add authorized emails: %s", observability.SafeError(err))
		writeAdminError(w, http.StatusInternalServerError, "Não foi possível atualizar a lista autorizada.")
		return
	}
	observability.Audit(r.Context(), "admin.registration.allowlist_updated", "actor_user_id", actor.ID, "emails_count", count)
	items, err := h.Registrations.ListAuthorizedEmails(r.Context())
	if err != nil {
		writeAdminError(w, http.StatusInternalServerError, "Lista atualizada, mas não foi possível recarregá-la.")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *AdminHandler) HandleDeleteAuthorizedEmail(w http.ResponseWriter, r *http.Request) {
	var payload authorizedEmailDeleteRequest
	if err := decodeAdminJSON(w, r, &payload); err != nil {
		writeAdminError(w, http.StatusBadRequest, "Payload JSON inválido.")
		return
	}
	email := strings.ToLower(strings.TrimSpace(payload.Email))
	if parsed, err := mail.ParseAddress(email); err != nil || parsed.Address != email {
		writeAdminError(w, http.StatusBadRequest, "Informe um e-mail válido.")
		return
	}
	if err := h.Registrations.DeleteAuthorizedEmail(r.Context(), email); err != nil {
		if errors.Is(err, repository.ErrAuthorizedEmailNotFound) {
			writeAdminError(w, http.StatusNotFound, "E-mail não encontrado na lista.")
			return
		}
		log.Printf("remove authorized email: %s", observability.SafeError(err))
		writeAdminError(w, http.StatusInternalServerError, "Não foi possível remover o e-mail autorizado.")
		return
	}
	actor, _ := middleware.UserFromContext(r.Context())
	observability.Audit(r.Context(), "admin.registration.allowlist_email_deleted", "actor_user_id", actor.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *AdminHandler) HandleReviewRegistration(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if !canonicalUserID.MatchString(userID) {
		writeAdminError(w, http.StatusBadRequest, "ID de utilizador inválido.")
		return
	}
	var payload registrationReviewRequest
	if err := decodeAdminJSON(w, r, &payload); err != nil {
		writeAdminError(w, http.StatusBadRequest, "Payload JSON inválido.")
		return
	}
	payload.Status = strings.TrimSpace(payload.Status)
	payload.Reason = strings.TrimSpace(payload.Reason)
	if payload.Status != repository.RegistrationApproved && payload.Status != repository.RegistrationRejected {
		writeAdminError(w, http.StatusBadRequest, "status deve ser approved ou rejected.")
		return
	}
	if payload.Status == repository.RegistrationRejected && (payload.Reason == "" || len([]rune(payload.Reason)) > 1000) {
		writeAdminError(w, http.StatusBadRequest, "A rejeição exige uma justificativa de até 1000 caracteres.")
		return
	}
	actor, _ := middleware.UserFromContext(r.Context())
	user, err := h.Registrations.ReviewUser(r.Context(), userID, actor.ID, payload.Status, payload.Reason)
	if err != nil {
		if errors.Is(err, repository.ErrRegistrationNotPending) {
			writeAdminError(w, http.StatusConflict, "Este cadastro já foi analisado ou não está pendente.")
			return
		}
		log.Printf("review registration: %s", observability.SafeError(err))
		writeAdminError(w, http.StatusInternalServerError, "Não foi possível registrar a decisão.")
		return
	}
	notificationSent := false
	if h.Notifier != nil {
		if notifyErr := h.Notifier.SendDecision(r.Context(), user.Email, user.Name, user.RegistrationStatus, user.RejectionReason); notifyErr != nil {
			log.Printf("send registration decision email: %s", observability.SafeError(notifyErr))
		} else {
			notificationSent = true
		}
	}
	observability.Audit(r.Context(), "admin.registration.reviewed", "actor_user_id", actor.ID, "target_user_id", user.ID, "decision", user.RegistrationStatus, "notification_sent", notificationSent)
	writeJSON(w, http.StatusOK, map[string]any{"user": user, "notification_sent": notificationSent})
}

func (h *AdminHandler) HandleResendRegistrationNotification(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if !canonicalUserID.MatchString(userID) {
		writeAdminError(w, http.StatusBadRequest, "ID de utilizador inválido.")
		return
	}
	user, err := h.Users.GetUserByID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeAdminError(w, http.StatusNotFound, "Utilizador não encontrado.")
			return
		}
		log.Printf("load registration notification target: %s", observability.SafeError(err))
		writeAdminError(w, http.StatusInternalServerError, "Não foi possível carregar o cadastro.")
		return
	}
	if user.RegistrationStatus != repository.RegistrationApproved && user.RegistrationStatus != repository.RegistrationRejected {
		writeAdminError(w, http.StatusConflict, "Só é possível notificar uma decisão já registrada.")
		return
	}
	if h.Notifier == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "Notificações SMTP não estão configuradas.")
		return
	}
	if err := h.Notifier.SendDecision(r.Context(), user.Email, user.Name, user.RegistrationStatus, user.RejectionReason); err != nil {
		log.Printf("resend registration decision email: %s", observability.SafeError(err))
		actor, _ := middleware.UserFromContext(r.Context())
		observability.Audit(r.Context(), "admin.registration.notification_resent", "actor_user_id", actor.ID, "target_user_id", user.ID, "decision", user.RegistrationStatus, "notification_sent", false)
		writeAdminError(w, http.StatusBadGateway, "A decisão está registrada, mas o provedor SMTP não aceitou o envio. Verifique a configuração e tente novamente.")
		return
	}
	actor, _ := middleware.UserFromContext(r.Context())
	observability.Audit(r.Context(), "admin.registration.notification_resent", "actor_user_id", actor.ID, "target_user_id", user.ID, "decision", user.RegistrationStatus, "notification_sent", true)
	writeJSON(w, http.StatusOK, map[string]any{"notification_sent": true})
}

func (h *AdminHandler) HandleUploadBatch(w http.ResponseWriter, r *http.Request) {
	if h.JobWorker == nil || h.Jobs == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "Fila de ingestão indisponível.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBatchBodyBytes)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeAdminError(w, http.StatusRequestEntityTooLarge, "O lote excede o limite total de 256 MiB.")
		} else {
			writeAdminError(w, http.StatusBadRequest, "Formulário multipart inválido.")
		}
		return
	}
	if r.MultipartForm != nil {
		defer func() { _ = r.MultipartForm.RemoveAll() }()
	}
	sourceRef := strings.TrimSpace(r.FormValue("source_reference"))
	if sourceRef == "" || len(sourceRef) > 2000 {
		writeAdminError(w, http.StatusBadRequest, "source_reference é obrigatório e deve ter até 2000 caracteres.")
		return
	}
	files := append([]*multipart.FileHeader(nil), r.MultipartForm.File["files"]...)
	files = append(files, r.MultipartForm.File["file"]...)
	if len(files) == 0 {
		writeAdminError(w, http.StatusBadRequest, "Envie ao menos um arquivo nos campos files ou file.")
		return
	}
	if len(files) > maxBatchFiles {
		writeAdminError(w, http.StatusBadRequest, fmt.Sprintf("O lote aceita até %d arquivos.", maxBatchFiles))
		return
	}
	var totalUploadBytes int64
	for _, header := range files {
		totalUploadBytes += header.Size
	}
	if totalUploadBytes > maxBatchTotalFileBytes {
		writeAdminError(w, http.StatusRequestEntityTooLarge, "O lote deve ter no máximo 200 MiB no total.")
		return
	}
	totalUploadBytes = 0 // Recount bytes actually copied, independent of multipart headers.
	fileTitle := strings.TrimSpace(r.FormValue("title"))
	if len(fileTitle) > 500 {
		writeAdminError(w, http.StatusBadRequest, "title deve ter no máximo 500 caracteres.")
		return
	}
	type stagedFile struct {
		task services.IngestionTask
	}
	staged := make([]stagedFile, 0, len(files))
	cleanupStaged := func() {
		for _, item := range staged {
			_ = os.Remove(item.task.TempPath)
		}
	}
	for _, header := range files {
		filename := filepath.Base(strings.ReplaceAll(header.Filename, "\\", "/"))
		extension := strings.ToLower(filepath.Ext(filename))
		fileType, supported := ingestionFileType(extension)
		if !supported {
			cleanupStaged()
			writeAdminError(w, http.StatusBadRequest, "Arquivos aceitos: .pdf, .txt, .md, .zip, .docx e .pptx.")
			return
		}
		if filename == "." || filename == "" || header.Size <= 0 || header.Size > maxBatchFileBytes {
			cleanupStaged()
			writeAdminError(w, http.StatusRequestEntityTooLarge, "Cada arquivo deve ter entre 1 byte e 200 MiB.")
			return
		}
		input, err := header.Open()
		if err != nil {
			cleanupStaged()
			writeAdminError(w, http.StatusBadRequest, "Não foi possível abrir um dos arquivos enviados.")
			return
		}
		tempFile, err := createIngestionTempFile("nodulo-ingestion-*" + extension)
		if err != nil {
			_ = input.Close()
			cleanupStaged()
			log.Printf("create ingestion temporary file: %v", err)
			writeAdminError(w, http.StatusInternalServerError, "Não foi possível preparar o arquivo para ingestão.")
			return
		}
		written, copyErr := io.Copy(tempFile, io.LimitReader(input, maxBatchFileBytes+1))
		closeInputErr := input.Close()
		syncErr := tempFile.Sync()
		closeTempErr := tempFile.Close()
		if copyErr != nil || closeInputErr != nil || syncErr != nil || closeTempErr != nil || written <= 0 || written > maxBatchFileBytes {
			_ = os.Remove(tempFile.Name())
			cleanupStaged()
			if written > maxBatchFileBytes {
				writeAdminError(w, http.StatusRequestEntityTooLarge, "Cada arquivo deve ter no máximo 200 MiB.")
			} else {
				writeAdminError(w, http.StatusBadRequest, "Não foi possível salvar um dos arquivos enviados.")
			}
			return
		}
		totalUploadBytes += written
		if totalUploadBytes > maxBatchTotalFileBytes {
			_ = os.Remove(tempFile.Name())
			cleanupStaged()
			writeAdminError(w, http.StatusRequestEntityTooLarge, "O lote deve ter no máximo 200 MiB no total.")
			return
		}
		staged = append(staged, stagedFile{task: services.IngestionTask{
			Filename: filename, Title: fileTitle, SourceReference: sourceRef, FileType: fileType, TempPath: tempFile.Name(),
		}})
	}

	jobInputs := make([]repository.IngestionJob, 0, len(staged))
	for _, item := range staged {
		jobInputs = append(jobInputs, repository.IngestionJob{
			Filename: item.task.Filename, Title: item.task.Title, SourceReference: sourceRef,
			FileType: item.task.FileType, PayloadPath: item.task.TempPath,
		})
	}
	created, err := h.Jobs.CreateJobsWithPayload(r.Context(), jobInputs)
	if err != nil {
		cleanupStaged()
		log.Printf("create batch ingestion jobs: %v", err)
		writeAdminError(w, http.StatusInternalServerError, "Não foi possível registrar os jobs de ingestão.")
		return
	}
	var queuedCount int
	for index := range staged {
		item := &staged[index]
		item.task.JobID = created[index].ID
		if err := h.JobWorker.Enqueue(item.task); err != nil {
			// The job and its durable source file are already in PostgreSQL/storage.
			// Keep it pending; the worker's database poll will pick it up.
			log.Printf("signal durable ingestion queue for job %s: %v", item.task.JobID, err)
		} else {
			queuedCount++
		}
	}
	actor, _ := middleware.UserFromContext(r.Context())
	jobIDs := make([]string, 0, len(created))
	for _, job := range created {
		jobIDs = append(jobIDs, job.ID)
	}
	observability.Audit(r.Context(), "admin.ingestion.batch.queued",
		"actor_user_id", actor.ID,
		"job_ids", jobIDs,
		"files_count", len(created),
		"queued_count", queuedCount,
		"total_bytes", totalUploadBytes,
		"source_reference_characters", len([]rune(sourceRef)),
	)
	writeJSON(w, http.StatusAccepted, map[string]any{"jobs": created, "created_count": len(created)})
}

func ingestionFileType(extension string) (string, bool) {
	switch extension {
	case ".pdf":
		return "pdf", true
	case ".zip":
		return "zip", true
	case ".docx":
		return "docx", true
	case ".pptx":
		return "pptx", true
	case ".txt", ".md":
		return "text", true
	default:
		return "", false
	}
}

func createIngestionTempFile(pattern string) (*os.File, error) {
	directory := strings.TrimSpace(os.Getenv("INGESTION_STORAGE_DIR"))
	if directory == "" {
		directory = filepath.Join("data", "ingestion")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create durable ingestion directory: %w", err)
	}
	file, err := os.CreateTemp(directory, pattern)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return nil, fmt.Errorf("restrict ingestion file permissions: %w", err)
	}
	return file, nil
}

type scrapeURLRequest struct {
	URL             string `json:"url"`
	Title           string `json:"title"`
	SourceReference string `json:"source_reference"`
}

func (h *AdminHandler) HandleScrapeURL(w http.ResponseWriter, r *http.Request) {
	if h.JobWorker == nil || h.Jobs == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "Fila de ingestão indisponível.")
		return
	}
	var payload scrapeURLRequest
	if err := decodeAdminJSON(w, r, &payload); err != nil {
		writeAdminError(w, http.StatusBadRequest, "Payload JSON inválido.")
		return
	}
	parsedURL, err := url.Parse(strings.TrimSpace(payload.URL))
	if err != nil || parsedURL == nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Hostname() == "" || parsedURL.User != nil || len(payload.URL) > 4096 {
		writeAdminError(w, http.StatusBadRequest, "url deve ser HTTP(S) e não pode conter credenciais.")
		return
	}
	if err := extractor.ValidatePublicURL(r.Context(), parsedURL.String()); err != nil {
		writeAdminError(w, http.StatusBadRequest, "URL inválida ou destino local/privado não permitido.")
		return
	}
	title := strings.TrimSpace(payload.Title)
	sourceRef := strings.TrimSpace(payload.SourceReference)
	if title == "" || sourceRef == "" || len(title) > 500 || len(sourceRef) > 2000 {
		writeAdminError(w, http.StatusBadRequest, "title e source_reference são obrigatórios e devem estar dentro dos limites permitidos.")
		return
	}
	job, err := h.Jobs.CreateJobWithPayload(r.Context(), title, title, sourceRef, "url", "", parsedURL.String())
	if err != nil {
		log.Printf("create URL ingestion job: %v", err)
		writeAdminError(w, http.StatusInternalServerError, "Não foi possível registrar o job de raspagem.")
		return
	}
	task := services.IngestionTask{JobID: job.ID, Filename: title, SourceReference: sourceRef, FileType: "url", URL: parsedURL.String()}
	if err := h.JobWorker.Enqueue(task); err != nil {
		log.Printf("signal durable ingestion queue for job %s: %v", job.ID, err)
	}
	actor, _ := middleware.UserFromContext(r.Context())
	observability.Audit(r.Context(), "admin.ingestion.url.queued", "actor_user_id", actor.ID, "job_id", job.ID, "host", parsedURL.Hostname())
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": job.ID, "status": "pending"})
}

func (h *AdminHandler) HandleListJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.Jobs.ListJobs(r.Context(), false)
	if err != nil {
		log.Printf("list ingestion jobs: %v", err)
		writeAdminError(w, http.StatusInternalServerError, "Não foi possível consultar os jobs de ingestão.")
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (h *AdminHandler) HandleRetryJob(w http.ResponseWriter, r *http.Request) {
	if h.JobWorker == nil || h.Jobs == nil {
		writeAdminError(w, http.StatusServiceUnavailable, "Fila de ingestão indisponível.")
		return
	}
	jobID := strings.TrimSpace(r.PathValue("id"))
	job, err := h.Jobs.GetJob(r.Context(), jobID)
	if errors.Is(err, repository.ErrNotFound) {
		writeAdminError(w, http.StatusNotFound, "Job de ingestão não encontrado.")
		return
	}
	if err != nil {
		log.Printf("load ingestion retry target: %v", err)
		writeAdminError(w, http.StatusInternalServerError, "Não foi possível carregar o job.")
		return
	}
	if job.Status != "failed" || job.FileType == "url" {
		writeAdminError(w, http.StatusConflict, "Só é possível retomar jobs de arquivo que falharam.")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBatchBodyBytes)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeAdminError(w, http.StatusRequestEntityTooLarge, "O arquivo excede o limite de 200 MiB.")
		} else {
			writeAdminError(w, http.StatusBadRequest, "Envie novamente o arquivo original em multipart/form-data.")
		}
		return
	}
	if r.MultipartForm != nil {
		defer func() { _ = r.MultipartForm.RemoveAll() }()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeAdminError(w, http.StatusBadRequest, "O campo file é obrigatório.")
		return
	}
	defer file.Close()
	filename := filepath.Base(strings.ReplaceAll(header.Filename, "\\", "/"))
	fileType, supported := ingestionFileType(strings.ToLower(filepath.Ext(filename)))
	if !supported || filename != job.Filename || fileType != job.FileType {
		writeAdminError(w, http.StatusBadRequest, "Selecione o mesmo arquivo original deste job.")
		return
	}
	if header.Size <= 0 || header.Size > maxBatchFileBytes {
		writeAdminError(w, http.StatusRequestEntityTooLarge, "O arquivo deve ter entre 1 byte e 200 MiB.")
		return
	}
	tempFile, err := createIngestionTempFile("nodulo-ingestion-retry-*" + strings.ToLower(filepath.Ext(filename)))
	if err != nil {
		log.Printf("create ingestion retry temporary file: %v", err)
		writeAdminError(w, http.StatusInternalServerError, "Não foi possível preparar o arquivo.")
		return
	}
	tempPath := tempFile.Name()
	written, copyErr := io.Copy(tempFile, io.LimitReader(file, maxBatchFileBytes+1))
	syncErr := tempFile.Sync()
	closeErr := tempFile.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil || written <= 0 || written > maxBatchFileBytes {
		_ = os.Remove(tempPath)
		writeAdminError(w, http.StatusBadRequest, "Não foi possível salvar o arquivo enviado.")
		return
	}

	job, err = h.Jobs.PrepareRetry(r.Context(), jobID, tempPath)
	if err != nil {
		_ = os.Remove(tempPath)
		if errors.Is(err, repository.ErrNotFound) {
			writeAdminError(w, http.StatusNotFound, "Job de ingestão não encontrado.")
		} else if errors.Is(err, repository.ErrJobNotRetryable) {
			writeAdminError(w, http.StatusConflict, "Este job já foi retomado ou não está mais em falha.")
		} else {
			log.Printf("prepare ingestion retry: %v", err)
			writeAdminError(w, http.StatusInternalServerError, "Não foi possível preparar a retomada.")
		}
		return
	}
	task := services.IngestionTask{
		JobID: job.ID, Filename: job.Filename, Title: job.Title, SourceReference: job.SourceReference,
		FileType: job.FileType, TempPath: tempPath, ResumeFrom: job.ProcessedChunks,
	}
	if err := h.JobWorker.Enqueue(task); err != nil {
		log.Printf("signal durable ingestion queue for retry %s: %v", job.ID, err)
	}
	actor, _ := middleware.UserFromContext(r.Context())
	observability.Audit(r.Context(), "admin.ingestion.job.resumed", "actor_user_id", actor.ID, "job_id", job.ID, "resume_from", job.ProcessedChunks)
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": job.ID, "status": job.Status, "processed_chunks": job.ProcessedChunks})
}

func (h *AdminHandler) HandleStreamJobs(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAdminError(w, http.StatusInternalServerError, "Streaming SSE não é suportado pelo servidor.")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		// Envia jobs recentes de todos os estados para que a interface receba
		// também a transição final para completed/failed.
		jobs, err := h.Jobs.ListJobs(r.Context(), false)
		if err != nil {
			writeSSE(w, flusher, "error", map[string]string{"error": "Não foi possível consultar os jobs recentes."})
			return
		}
		writeSSE(w, flusher, "jobs", jobs)
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

var canonicalUserID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type updateUserRoleRequest struct {
	Role string `json:"role"`
}

func (h *AdminHandler) HandleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.Repository.ListUsers(r.Context())
	if err != nil {
		log.Printf("list users: %v", err)
		writeAdminError(w, http.StatusInternalServerError, "Não foi possível listar os utilizadores.")
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (h *AdminHandler) HandleUpdateUserRole(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if !canonicalUserID.MatchString(userID) {
		writeAdminError(w, http.StatusBadRequest, "ID de utilizador inválido.")
		return
	}
	var payload updateUserRoleRequest
	if err := decodeAdminJSON(w, r, &payload); err != nil {
		writeAdminError(w, http.StatusBadRequest, "Payload JSON inválido.")
		return
	}
	actor, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeAdminError(w, http.StatusUnauthorized, "Autenticação obrigatória.")
		return
	}
	if actor.ID == userID && payload.Role != "admin" {
		writeAdminError(w, http.StatusConflict, "Não é possível rebaixar a própria conta administrativa.")
		return
	}
	user, err := h.Repository.UpdateUserRole(r.Context(), userID, payload.Role)
	if err != nil {
		h.writeUserMutationError(w, err, "Não foi possível atualizar o papel do utilizador.")
		return
	}
	observability.Audit(r.Context(), "admin.user.role_updated", "actor_user_id", actor.ID, "target_user_id", user.ID, "role", user.Role)
	writeJSON(w, http.StatusOK, user)
}

func (h *AdminHandler) HandleDeleteUser(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if !canonicalUserID.MatchString(userID) {
		writeAdminError(w, http.StatusBadRequest, "ID de utilizador inválido.")
		return
	}
	actor, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeAdminError(w, http.StatusUnauthorized, "Autenticação obrigatória.")
		return
	}
	if err := h.Repository.DeleteUser(r.Context(), actor.ID, userID); err != nil {
		h.writeUserMutationError(w, err, "Não foi possível remover o utilizador.")
		return
	}
	observability.Audit(r.Context(), "admin.user.deleted", "actor_user_id", actor.ID, "target_user_id", userID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *AdminHandler) writeUserMutationError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		writeAdminError(w, http.StatusNotFound, "Utilizador não encontrado.")
	case errors.Is(err, repository.ErrInvalidRole):
		writeAdminError(w, http.StatusBadRequest, "O papel deve ser admin ou user.")
	case errors.Is(err, repository.ErrLastAdmin):
		writeAdminError(w, http.StatusConflict, "A operação não pode remover ou rebaixar o último administrador.")
	case errors.Is(err, repository.ErrCannotDeleteSelf):
		writeAdminError(w, http.StatusConflict, "Não é possível remover a própria conta.")
	default:
		log.Printf("admin user operation failed: %v", err)
		writeAdminError(w, http.StatusInternalServerError, fallback)
	}
}

func (h *AdminHandler) HandleGetStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.Repository.GetStats(r.Context())
	if err != nil {
		log.Printf("load administration statistics: %v", err)
		writeAdminError(w, http.StatusInternalServerError, "Não foi possível carregar as métricas do sistema.")
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (h *AdminHandler) HandleListModels(w http.ResponseWriter, r *http.Request) {
	models, err := h.OllamaClient.ListLocalModels(r.Context())
	if err != nil {
		log.Printf("list Ollama models: %s", observability.SafeError(err))
		writeAdminError(w, http.StatusBadGateway, "Não foi possível consultar os modelos do Ollama.")
		return
	}
	writeJSON(w, http.StatusOK, models)
}

func (h *AdminHandler) HandlePullModel(w http.ResponseWriter, r *http.Request) {
	var payload modelRequest
	if err := decodeAdminJSON(w, r, &payload); err != nil {
		writeAdminError(w, http.StatusBadRequest, "Payload JSON inválido.")
		return
	}
	modelName, ok := validateModelName(payload.Model)
	if !ok {
		writeAdminError(w, http.StatusBadRequest, "model é obrigatório e deve ser um nome válido.")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAdminError(w, http.StatusInternalServerError, "Streaming SSE não é suportado pelo servidor.")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Accel-Buffering", "no")

	actor, _ := middleware.UserFromContext(r.Context())
	observability.Audit(r.Context(), "admin.model.pull.started", "actor_user_id", actor.ID, "model", modelName)
	progress, errs := h.OllamaClient.PullModelStream(r.Context(), modelName)
	failed := false
	for progress != nil || errs != nil {
		select {
		case <-r.Context().Done():
			observability.Audit(r.Context(), "admin.model.pull.cancelled", "actor_user_id", actor.ID, "model", modelName)
			return
		case item, open := <-progress:
			if !open {
				progress = nil
				continue
			}
			writeSSE(w, flusher, "progress", item)
		case err, open := <-errs:
			if !open {
				errs = nil
				continue
			}
			if err != nil {
				failed = true
				observability.Audit(r.Context(), "admin.model.pull.failed", "actor_user_id", actor.ID, "model", modelName, "error", observability.SafeError(err))
				writeSSE(w, flusher, "error", map[string]string{"error": err.Error()})
				writeSSEDone(w, flusher)
				return
			}
		}
	}
	if !failed {
		observability.Audit(r.Context(), "admin.model.pull.completed", "actor_user_id", actor.ID, "model", modelName)
	}
	writeSSEDone(w, flusher)
}

func (h *AdminHandler) HandleDeleteModel(w http.ResponseWriter, r *http.Request) {
	var payload modelRequest
	if err := decodeAdminJSON(w, r, &payload); err != nil {
		writeAdminError(w, http.StatusBadRequest, "Payload JSON inválido.")
		return
	}
	modelName, ok := validateModelName(payload.Model)
	if !ok {
		writeAdminError(w, http.StatusBadRequest, "model é obrigatório e deve ser um nome válido.")
		return
	}
	if err := h.OllamaClient.DeleteModel(r.Context(), modelName); err != nil {
		log.Printf("delete Ollama model %q failed: %s", modelName, observability.SafeError(err))
		writeAdminError(w, http.StatusBadGateway, "Não foi possível remover o modelo do Ollama.")
		return
	}
	actor, _ := middleware.UserFromContext(r.Context())
	observability.Audit(r.Context(), "admin.model.deleted", "actor_user_id", actor.ID, "model", modelName)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "model": modelName})
}

func (h *AdminHandler) HandleListDocuments(w http.ResponseWriter, r *http.Request) {
	documents, err := h.Repository.ListIndexedDocuments(r.Context())
	if err != nil {
		log.Printf("list indexed documents: %v", err)
		writeAdminError(w, http.StatusInternalServerError, "Não foi possível listar os documentos indexados.")
		return
	}
	writeJSON(w, http.StatusOK, documents)
}

func (h *AdminHandler) HandleDeleteDocument(w http.ResponseWriter, r *http.Request) {
	var payload deleteDocumentRequest
	if err := decodeAdminJSON(w, r, &payload); err != nil {
		writeAdminError(w, http.StatusBadRequest, "Payload JSON inválido.")
		return
	}
	sourceRef := strings.TrimSpace(payload.SourceReference)
	if sourceRef == "" {
		writeAdminError(w, http.StatusBadRequest, "source_reference é obrigatório.")
		return
	}
	if err := h.Repository.DeleteDocumentBySource(r.Context(), sourceRef); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeAdminError(w, http.StatusNotFound, "Nenhum documento corresponde a essa referência.")
			return
		}
		log.Printf("delete indexed document failed (reference length=%d): %s", len([]rune(sourceRef)), observability.SafeError(err))
		writeAdminError(w, http.StatusInternalServerError, "Não foi possível remover o documento indexado.")
		return
	}
	actor, _ := middleware.UserFromContext(r.Context())
	observability.Audit(r.Context(), "admin.document.deleted", "actor_user_id", actor.ID, "source_reference_characters", len([]rune(sourceRef)))
	w.WriteHeader(http.StatusNoContent)
}

func (h *AdminHandler) HandleNotFound(w http.ResponseWriter, _ *http.Request) {
	writeAdminError(w, http.StatusNotFound, "Rota administrativa não encontrada.")
}

func decodeAdminJSON(w http.ResponseWriter, r *http.Request, target any) error {
	return decodeJSONBody(w, r, target, maxAdminJSONBytes)
}

func validateModelName(value string) (string, bool) {
	name := strings.TrimSpace(value)
	if name == "" || len(name) > 256 || strings.ContainsAny(name, "\r\n\x00") {
		return "", false
	}
	return name, true
}

func writeSSEDone(w http.ResponseWriter, flusher http.Flusher) {
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func writeAdminError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
