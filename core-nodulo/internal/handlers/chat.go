package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"core-nodulo/internal/agent"
	"core-nodulo/internal/middleware"
	"core-nodulo/internal/observability"
	"core-nodulo/internal/repository"
	"core-nodulo/internal/services"
	"core-nodulo/pkg/ollama"
)

type ChatHandler struct {
	OllamaClient   *ollama.Client
	Repository     *repository.ChatRepository
	UploadSettings *repository.ChatUploadSettingsRepository
	RAG            *services.RAGService
	Memory         *services.MemoryService
	TitleGenerator *services.TitleGenerator
	DefaultModel   string
}

func (h *ChatHandler) SetMemoryService(memory *services.MemoryService) {
	h.Memory = memory
}

func (h *ChatHandler) SetTitleGenerator(generator *services.TitleGenerator) {
	h.TitleGenerator = generator
}

const maxGeneratedImageStorageBytes = 8 << 20

type ChatPayload struct {
	ConversationID string `json:"conversation_id,omitempty"`
	RetryMessageID string `json:"retry_message_id,omitempty"`
	Prompt         string `json:"prompt"`
	Model          string `json:"model,omitempty"`
	Thinking       bool   `json:"thinking,omitempty"`
}

type conversationMeta struct {
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
}

type sourceMetadata struct {
	Type            string `json:"type"`
	Title           string `json:"title"`
	SourceReference string `json:"source_reference,omitempty"`
	URL             string `json:"url,omitempty"`
}

type documentIngestPayload struct {
	Title           string `json:"title"`
	SourceReference string `json:"source_reference"`
	Content         string `json:"content"`
}

type conversationFolderPayload struct {
	Name string `json:"name"`
}

type moveConversationFolderPayload struct {
	FolderID *string `json:"folder_id"`
}

type archiveConversationPayload struct {
	Archived *bool `json:"archived"`
}

func NewChatHandler(client *ollama.Client, repo *repository.ChatRepository, uploadSettings *repository.ChatUploadSettingsRepository, rag *services.RAGService, model string) *ChatHandler {
	return &ChatHandler{
		OllamaClient:   client,
		Repository:     repo,
		UploadSettings: uploadSettings,
		RAG:            rag,
		DefaultModel:   model,
	}
}

func (h *ChatHandler) HandleChat(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming não suportado pelo servidor", http.StatusInternalServerError)
		return
	}
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Autenticação obrigatória.", http.StatusUnauthorized)
		return
	}

	uploadSettings := repository.ChatUploadSettings{}
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
		if h.UploadSettings == nil {
			http.Error(w, "A configuração de anexos está indisponível.", http.StatusServiceUnavailable)
			return
		}
		var err error
		uploadSettings, err = h.UploadSettings.Get(r.Context())
		if err != nil {
			slog.ErrorContext(r.Context(), "load chat upload limits", "error", observability.SafeError(err))
			http.Error(w, "Não foi possível consultar os limites atuais de anexos.", http.StatusServiceUnavailable)
			return
		}
	}
	payload, attachments, attachmentMetadata, multipartForm, err := parseChatInput(w, r, uploadSettings)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errChatUploadTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, err.Error(), status)
		return
	}
	if multipartForm != nil {
		defer multipartForm.RemoveAll()
	}
	prompt := strings.TrimSpace(payload.Prompt)
	if prompt == "" && len(attachments) > 0 {
		prompt = defaultFileOnlyPrompt
		for _, attachment := range attachments {
			if attachment.ImageBase64 != "" {
				prompt = defaultImageOnlyPrompt
				break
			}
		}
	}
	model := strings.TrimSpace(payload.Model)
	if model == "" {
		model = h.DefaultModel
	}
	var imagePayloads []string
	for _, attachment := range attachments {
		if attachment.ImageBase64 == "" {
			continue
		}
		if h.RAG == nil || strings.TrimSpace(h.RAG.VisionModel) == "" {
			http.Error(w, "A análise de imagens não está configurada. Defina VISION_MODEL e reinicie a API.", http.StatusServiceUnavailable)
			return
		}
		model = h.RAG.VisionModel
		imagePayloads = append(imagePayloads, attachment.ImageBase64)
	}

	conversationID := strings.TrimSpace(payload.ConversationID)
	newConversation := false
	retryMessageID := strings.TrimSpace(payload.RetryMessageID)
	if retryMessageID != "" && conversationID == "" {
		http.Error(w, "Não foi possível identificar a conversa desta mensagem. Atualize o histórico e tente novamente.", http.StatusBadRequest)
		return
	}
	if retryMessageID != "" && !isUUID(retryMessageID) {
		http.Error(w, "ID da mensagem inválido.", http.StatusBadRequest)
		return
	}
	if conversationID == "" {
		var err error
		title := prompt
		if strings.TrimSpace(payload.Prompt) == "" && len(attachmentMetadata) > 0 {
			title = attachmentMetadata[0].Name
		}
		conversationID, err = h.Repository.CreateConversation(r.Context(), user.ID, titleFromPrompt(title))
		if err != nil {
			http.Error(w, "Não foi possível criar a conversa.", http.StatusInternalServerError)
			return
		}
		newConversation = true
	} else {
		if !isUUID(conversationID) {
			http.Error(w, "conversation_id inválido.", http.StatusBadRequest)
			return
		}
		_, err := h.Repository.GetConversationForUser(r.Context(), conversationID, user.ID)
		if err != nil {
			writeRepositoryError(w, err)
			return
		}
	}
	beforeMessageID := ""
	if retryMessageID != "" {
		beforeMessageID = retryMessageID
	}
	recentMessages, err := h.Repository.GetRecentMessagesForContext(r.Context(), conversationID, user.ID, beforeMessageID, 10)
	if err != nil {
		slog.ErrorContext(r.Context(), "load recent chat context", "conversation_id", conversationID, "error", observability.SafeError(err))
		http.Error(w, "Não foi possível carregar o contexto recente desta conversa.", http.StatusInternalServerError)
		return
	}
	history := recentMessagesToLLM(recentMessages)
	var memoryFacts []string
	if h.Memory != nil {
		memoryFacts, err = h.Memory.RelevantFacts(r.Context(), user.ID, prompt)
		if err != nil {
			slog.WarnContext(r.Context(), "semantic memory lookup unavailable; continuing without profile context",
				"user_id", user.ID, "reason", observability.SafeError(err))
			memoryFacts = nil
		}
	}

	var messageID string
	if retryMessageID != "" {
		if err := h.Repository.ReplaceUserMessageForRetry(r.Context(), user.ID, conversationID, retryMessageID, strings.TrimSpace(payload.Prompt), attachmentMetadata); err != nil {
			writeRepositoryError(w, err)
			return
		}
		messageID = retryMessageID
		observability.Audit(r.Context(), "chat.message.edited_for_retry", "user_id", user.ID, "conversation_id", conversationID, "message_id", messageID)
	} else {
		var err error
		messageID, err = h.Repository.SaveMessageWithAttachmentsAndID(r.Context(), conversationID, "user", strings.TrimSpace(payload.Prompt), attachmentMetadata)
		if err != nil {
			writeRepositoryError(w, err)
			return
		}
	}
	if newConversation && h.TitleGenerator != nil {
		go h.TitleGenerator.GenerateAndUpdateTitle(conversationID, prompt)
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	writeSSE(w, flusher, "meta", conversationMeta{ConversationID: conversationID, MessageID: messageID})

	generationPrompt := prompt
	var tokens <-chan string
	var errs <-chan error
	var sources []services.ChatSource
	generatedImages := make([]repository.MessageAttachment, 0, 1)
	var generatedImageBytes int64
	if len(imagePayloads) > 0 {
		if h.OllamaClient == nil {
			writeSSE(w, flusher, "error", map[string]string{"error": "O serviço de análise de imagens está indisponível no momento."})
			return
		}
		// Keep image analysis isolated from old text-only turns, memory, RAG and
		// ReAct research so the vision model sees precisely this question and image.
		messages := []ollama.ChatMessage{
			{Role: "system", Content: services.VisionAssistantSystemPrompt},
			{Role: "user", Content: generationPrompt, Images: imagePayloads},
		}
		tokens, errs = h.OllamaClient.StreamChat(r.Context(), model, messages)
	} else {
		if h.RAG == nil {
			writeSSE(w, flusher, "error", map[string]string{"error": "O serviço de geração está indisponível no momento."})
			return
		}
		progress := func(event agent.ProgressEvent) {
			if r.Context().Err() == nil {
				if event.Attachment != "" {
					const pngDataPrefix = "data:image/png;base64,"
					if strings.HasPrefix(event.Attachment, pngDataPrefix) {
						encoded := strings.TrimPrefix(event.Attachment, pngDataPrefix)
						decoded, decodeErr := base64.StdEncoding.DecodeString(encoded)
						if decodeErr == nil && generatedImageBytes+int64(len(decoded)) <= maxGeneratedImageStorageBytes {
							generatedImages = append(generatedImages, repository.MessageAttachment{
								Name: fmt.Sprintf("grafico-%d.png", len(generatedImages)+1), Size: int64(len(decoded)),
								DataURL: event.Attachment, MimeType: "image/png", Generated: true,
							})
							generatedImageBytes += int64(len(decoded))
							writeSSE(w, flusher, "attachment", map[string]string{"data_url": event.Attachment})
						} else {
							event.Message = "O gráfico foi criado, mas excedeu o limite de armazenamento desta resposta."
						}
					}
					event.Attachment = ""
				}
				writeSSE(w, flusher, "research", event)
			}
		}
		var err error
		tokens, errs, sources, err = h.RAG.StreamChatWithAttachmentsThinkingProgressAndHistoryAndMemory(
			r.Context(), model, generationPrompt, attachments, payload.Thinking, history, memoryFacts, progress,
		)
		if err != nil {
			slog.ErrorContext(r.Context(), "prepare chat response failed", "error", observability.SafeError(err), "model", model)
			message := "Não foi possível preparar a resposta. Corrija a solicitação ou tente novamente."
			if strings.Contains(strings.ToLower(err.Error()), "execute_python") || strings.Contains(strings.ToLower(err.Error()), "python sandbox") {
				message = "A ferramenta de cálculos não está disponível. O ambiente isolado de Python não está configurado; avise o administrador da instalação."
			}
			writeSSE(w, flusher, "error", map[string]string{"error": message})
			return
		}
	}
	if len(sources) > 0 {
		metadata := make([]sourceMetadata, 0, len(sources))
		for _, source := range sources {
			metadata = append(metadata, sourceMetadata{
				Type: source.Type, Title: source.Title,
				SourceReference: source.SourceReference, URL: source.URL,
			})
		}
		writeSSE(w, flusher, "sources", metadata)
	}

	var answer strings.Builder
	observability.Audit(r.Context(), "chat.generation.started",
		"user_id", user.ID,
		"conversation_id", conversationID,
		"model", model,
		"prompt_characters", len([]rune(prompt)),
		"attachment_count", len(attachments),
		"rag_sources", len(sources),
		"thinking_enabled", payload.Thinking,
	)
	for tokens != nil || errs != nil {
		select {
		case <-r.Context().Done():
			if answer.Len() > 0 || len(generatedImages) > 0 {
				if err := h.saveAssistantMessage(conversationID, answer.String(), generatedImages); err != nil {
					slog.ErrorContext(r.Context(), "persist partial assistant response failed", "conversation_id", conversationID, "error", observability.SafeError(err))
				}
			}
			observability.Audit(r.Context(), "chat.generation.interrupted",
				"user_id", user.ID,
				"conversation_id", conversationID,
				"model", model,
				"response_characters", len([]rune(answer.String())),
				"elapsed_ms", time.Since(startedAt).Milliseconds(),
			)
			return
		case err, open := <-errs:
			if !open {
				errs = nil
				continue
			}
			if err != nil {
				slog.ErrorContext(r.Context(), "chat generation stream failed", "conversation_id", conversationID, "model", model, "error", observability.SafeError(err))
				if answer.Len() > 0 || len(generatedImages) > 0 {
					if saveErr := h.saveAssistantMessage(conversationID, answer.String(), generatedImages); saveErr != nil {
						slog.ErrorContext(r.Context(), "persist partial assistant response failed", "conversation_id", conversationID, "error", observability.SafeError(saveErr))
					}
				}
				observability.Audit(r.Context(), "chat.generation.failed",
					"user_id", user.ID,
					"conversation_id", conversationID,
					"model", model,
					"response_characters", len([]rune(answer.String())),
					"elapsed_ms", time.Since(startedAt).Milliseconds(),
				)
				writeSSE(w, flusher, "error", map[string]string{"error": chatGenerationErrorMessage(err)})
				return
			}
		case token, open := <-tokens:
			if !open {
				tokens = nil
				continue
			}
			answer.WriteString(token)
			writeSSE(w, flusher, "", map[string]string{"text": token})
		}
	}

	if err := h.saveAssistantMessage(conversationID, answer.String(), generatedImages); err != nil {
		observability.Audit(r.Context(), "chat.generation.persistence_failed", "user_id", user.ID, "conversation_id", conversationID, "model", model, "response_characters", len([]rune(answer.String())))
		writeSSE(w, flusher, "error", map[string]string{"error": "Não foi possível salvar a resposta."})
		return
	}
	observability.Audit(r.Context(), "chat.generation.completed",
		"user_id", user.ID,
		"conversation_id", conversationID,
		"model", model,
		"response_characters", len([]rune(answer.String())),
		"elapsed_ms", time.Since(startedAt).Milliseconds(),
	)
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
	if h.Memory != nil && strings.TrimSpace(prompt) != "" && strings.TrimSpace(answer.String()) != "" {
		go h.Memory.ExtractAndSaveFact(user.ID, prompt, answer.String())
	}
}

// chatGenerationErrorMessage turns common inference-runtime failures into
// actionable Portuguese messages without exposing provider internals to users.
func chatGenerationErrorMessage(err error) string {
	if err == nil {
		return "Não foi possível gerar a resposta. Tente novamente."
	}

	message := strings.ToLower(err.Error())
	if strings.Contains(message, "unknown model architecture") && strings.Contains(message, "mllama") {
		return "O serviço de IA configurado não oferece suporte ao modelo de visão selecionado. Atualize o runtime de inferência para uma versão compatível com Llama 3.2 Vision ou configure outro modelo visual compatível."
	}

	return "Não foi possível gerar a resposta. Verifique a disponibilidade do serviço de IA e tente novamente."
}

func recentMessagesToLLM(messages []repository.Message) []ollama.ChatMessage {
	const maxContextRunes = 40_000
	remaining := maxContextRunes
	reversed := make([]ollama.ChatMessage, 0, len(messages))
	for index := len(messages) - 1; index >= 0 && remaining > 0; index-- {
		message := messages[index]
		if message.Role != "user" && message.Role != "assistant" {
			continue
		}
		content := strings.TrimSpace(message.Content)
		runes := []rune(content)
		if len(runes) > remaining {
			runes = runes[len(runes)-remaining:]
		}
		content = string(runes)
		if content == "" {
			continue
		}
		reversed = append(reversed, ollama.ChatMessage{Role: message.Role, Content: content})
		remaining -= len(runes)
	}
	history := make([]ollama.ChatMessage, len(reversed))
	for index := range reversed {
		history[len(reversed)-1-index] = reversed[index]
	}
	return history
}

func (h *ChatHandler) HandleIngestDocument(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return
	}
	if h.RAG == nil {
		http.Error(w, "Serviço de ingestão indisponível.", http.StatusServiceUnavailable)
		return
	}

	var payload documentIngestPayload
	if err := decodeJSONBody(w, r, &payload, 16<<20); err != nil {
		http.Error(w, "Payload inválido ou excede 16 MiB.", http.StatusBadRequest)
		return
	}
	payload.Title = strings.TrimSpace(payload.Title)
	payload.SourceReference = strings.TrimSpace(payload.SourceReference)
	if payload.Title == "" || payload.SourceReference == "" || strings.TrimSpace(payload.Content) == "" {
		http.Error(w, "title, source_reference e content são obrigatórios.", http.StatusBadRequest)
		return
	}

	chunksCount, err := h.RAG.IngestText(r.Context(), payload.Title, payload.SourceReference, payload.Content)
	if err != nil {
		slog.ErrorContext(r.Context(), "document ingestion request failed", "error", observability.SafeError(err))
		http.Error(w, "Não foi possível ingerir o documento.", http.StatusBadGateway)
		return
	}
	user, _ := middleware.UserFromContext(r.Context())
	observability.Audit(r.Context(), "admin.document.ingested", "actor_user_id", user.ID, "title_characters", len([]rune(payload.Title)), "source_reference_characters", len([]rune(payload.SourceReference)), "content_characters", len([]rune(payload.Content)), "chunks_count", chunksCount)
	writeJSON(w, http.StatusCreated, map[string]any{
		"status":       "ingested",
		"chunks_count": chunksCount,
	})
}

func (h *ChatHandler) HandleListConversations(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Autenticação obrigatória.", http.StatusUnauthorized)
		return
	}
	conversations, err := h.Repository.ListConversations(r.Context(), user.ID)
	if err != nil {
		http.Error(w, "Não foi possível listar as conversas.", http.StatusInternalServerError)
		return
	}
	observability.Audit(r.Context(), "chat.conversations.listed", "user_id", user.ID, "count", len(conversations))
	writeJSON(w, http.StatusOK, conversations)
}

func (h *ChatHandler) HandleListConversationFolders(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Autenticação obrigatória.", http.StatusUnauthorized)
		return
	}
	folders, err := h.Repository.ListConversationFolders(r.Context(), user.ID)
	if err != nil {
		http.Error(w, "Não foi possível listar as áreas de trabalho.", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, folders)
}

func (h *ChatHandler) HandleCreateConversationFolder(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Autenticação obrigatória.", http.StatusUnauthorized)
		return
	}
	var payload conversationFolderPayload
	if err := decodeConversationJSON(w, r, &payload); err != nil {
		http.Error(w, "Envie um JSON válido com o campo name.", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" || len([]rune(name)) > 100 {
		http.Error(w, "O nome da área de trabalho deve ter entre 1 e 100 caracteres.", http.StatusBadRequest)
		return
	}
	folder, err := h.Repository.CreateConversationFolder(r.Context(), user.ID, name)
	if err != nil {
		if errors.Is(err, repository.ErrConflict) {
			http.Error(w, "Já existe uma área de trabalho com esse nome.", http.StatusConflict)
			return
		}
		http.Error(w, "Não foi possível criar a área de trabalho.", http.StatusInternalServerError)
		return
	}
	observability.Audit(r.Context(), "chat.folder.created", "user_id", user.ID, "folder_id", folder.ID)
	writeJSON(w, http.StatusCreated, folder)
}

func (h *ChatHandler) HandleRenameConversationFolder(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Autenticação obrigatória.", http.StatusUnauthorized)
		return
	}
	folderID := r.PathValue("id")
	if !isUUID(folderID) {
		http.Error(w, "ID da área de trabalho inválido.", http.StatusBadRequest)
		return
	}
	var payload conversationFolderPayload
	if err := decodeConversationJSON(w, r, &payload); err != nil {
		http.Error(w, "Envie um JSON válido com o campo name.", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" || len([]rune(name)) > 100 {
		http.Error(w, "O nome da área de trabalho deve ter entre 1 e 100 caracteres.", http.StatusBadRequest)
		return
	}
	folder, err := h.Repository.RenameConversationFolder(r.Context(), user.ID, folderID, name)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			http.Error(w, "Área de trabalho não encontrada.", http.StatusNotFound)
		case errors.Is(err, repository.ErrConflict):
			http.Error(w, "Já existe uma área de trabalho com esse nome.", http.StatusConflict)
		default:
			http.Error(w, "Não foi possível renomear a área de trabalho.", http.StatusInternalServerError)
		}
		return
	}
	observability.Audit(r.Context(), "chat.folder.renamed", "user_id", user.ID, "folder_id", folder.ID)
	writeJSON(w, http.StatusOK, folder)
}

func (h *ChatHandler) HandleDeleteConversationFolder(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Autenticação obrigatória.", http.StatusUnauthorized)
		return
	}
	folderID := r.PathValue("id")
	if !isUUID(folderID) {
		http.Error(w, "ID da área de trabalho inválido.", http.StatusBadRequest)
		return
	}
	if err := h.Repository.DeleteConversationFolder(r.Context(), user.ID, folderID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			http.Error(w, "Área de trabalho não encontrada.", http.StatusNotFound)
			return
		}
		http.Error(w, "Não foi possível excluir a área de trabalho.", http.StatusInternalServerError)
		return
	}
	observability.Audit(r.Context(), "chat.folder.deleted", "user_id", user.ID, "folder_id", folderID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *ChatHandler) HandleMoveConversation(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Autenticação obrigatória.", http.StatusUnauthorized)
		return
	}
	conversationID := r.PathValue("id")
	if !isUUID(conversationID) {
		http.Error(w, "ID de conversa inválido.", http.StatusBadRequest)
		return
	}
	var payload moveConversationFolderPayload
	if err := decodeConversationJSON(w, r, &payload); err != nil {
		http.Error(w, "Envie um JSON válido com folder_id ou null.", http.StatusBadRequest)
		return
	}
	if payload.FolderID != nil {
		folderID := strings.TrimSpace(*payload.FolderID)
		if folderID == "" {
			payload.FolderID = nil
		} else if !isUUID(folderID) {
			http.Error(w, "ID da área de trabalho inválido.", http.StatusBadRequest)
			return
		} else {
			payload.FolderID = &folderID
		}
	}
	if err := h.Repository.MoveConversationToFolder(r.Context(), user.ID, conversationID, payload.FolderID); err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			http.Error(w, "Conversa não encontrada.", http.StatusNotFound)
		case errors.Is(err, repository.ErrInvalidFolder):
			http.Error(w, "A área de trabalho não existe ou não pertence a esta conta.", http.StatusBadRequest)
		default:
			http.Error(w, "Não foi possível mover a conversa.", http.StatusInternalServerError)
		}
		return
	}
	observability.Audit(r.Context(), "chat.conversation.folder_changed", "user_id", user.ID, "conversation_id", conversationID, "folder_id", payload.FolderID)
	writeJSON(w, http.StatusOK, map[string]any{"status": "moved", "folder_id": payload.FolderID})
}

func (h *ChatHandler) HandleListArchivedConversations(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Autenticação obrigatória.", http.StatusUnauthorized)
		return
	}
	conversations, err := h.Repository.ListArchivedConversations(r.Context(), user.ID)
	if err != nil {
		http.Error(w, "Não foi possível listar as conversas arquivadas.", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, conversations)
}

func (h *ChatHandler) HandleArchiveConversation(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Autenticação obrigatória.", http.StatusUnauthorized)
		return
	}
	conversationID := r.PathValue("id")
	if !isUUID(conversationID) {
		http.Error(w, "ID de conversa inválido.", http.StatusBadRequest)
		return
	}
	var payload archiveConversationPayload
	if err := decodeConversationJSON(w, r, &payload); err != nil || payload.Archived == nil {
		http.Error(w, "Envie archived como true ou false.", http.StatusBadRequest)
		return
	}
	if err := h.Repository.SetConversationArchived(r.Context(), user.ID, conversationID, *payload.Archived); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			http.Error(w, "Conversa não encontrada.", http.StatusNotFound)
			return
		}
		http.Error(w, "Não foi possível atualizar o estado da conversa.", http.StatusInternalServerError)
		return
	}
	event := "chat.conversation.unarchived"
	status := "unarchived"
	if *payload.Archived {
		event = "chat.conversation.archived"
		status = "archived"
	}
	observability.Audit(r.Context(), event, "user_id", user.ID, "conversation_id", conversationID)
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

func (h *ChatHandler) HandleDeleteConversation(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Autenticação obrigatória.", http.StatusUnauthorized)
		return
	}
	conversationID := r.PathValue("id")
	if !isUUID(conversationID) {
		http.Error(w, "ID de conversa inválido.", http.StatusBadRequest)
		return
	}
	if err := h.Repository.DeleteConversation(r.Context(), user.ID, conversationID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			http.Error(w, "Conversa não encontrada.", http.StatusNotFound)
			return
		}
		http.Error(w, "Não foi possível excluir a conversa.", http.StatusInternalServerError)
		return
	}
	observability.Audit(r.Context(), "chat.conversation.deleted", "user_id", user.ID, "conversation_id", conversationID)
	w.WriteHeader(http.StatusNoContent)
}

func decodeConversationJSON(w http.ResponseWriter, r *http.Request, target any) error {
	return decodeJSONBody(w, r, target, 1<<16)
}

func (h *ChatHandler) HandleGetMessages(w http.ResponseWriter, r *http.Request) {
	conversationID := r.PathValue("id")
	if !isUUID(conversationID) {
		http.Error(w, "ID de conversa inválido.", http.StatusBadRequest)
		return
	}
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Autenticação obrigatória.", http.StatusUnauthorized)
		return
	}
	if _, err := h.Repository.GetConversationForUser(r.Context(), conversationID, user.ID); err != nil {
		writeRepositoryError(w, err)
		return
	}
	messages, err := h.Repository.GetMessages(r.Context(), conversationID)
	if err != nil {
		http.Error(w, "Não foi possível recuperar as mensagens.", http.StatusInternalServerError)
		return
	}
	observability.Audit(r.Context(), "chat.conversation.history_read", "user_id", user.ID, "conversation_id", conversationID, "messages_count", len(messages))
	writeJSON(w, http.StatusOK, messages)
}

func (h *ChatHandler) saveAssistantMessage(conversationID, content string, attachments []repository.MessageAttachment) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return h.Repository.SaveMessageWithAttachments(ctx, conversationID, "assistant", content, attachments)
}

func writeRepositoryError(w http.ResponseWriter, err error) {
	if errors.Is(err, repository.ErrNotFound) {
		http.Error(w, "Conversa não encontrada.", http.StatusNotFound)
		return
	}
	http.Error(w, "Falha ao acessar a conversa.", http.StatusInternalServerError)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		return
	}
}

func titleFromPrompt(prompt string) string {
	title := []rune(strings.TrimSpace(prompt))
	if len(title) > 40 {
		return string(title[:40])
	}
	return string(title)
}

func isUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, char := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if char != '-' {
				return false
			}
			continue
		}
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
			return false
		}
	}
	return true
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, event string, payload any) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return
	}
	if event != "" {
		fmt.Fprintf(w, "event: %s\n", event)
	}
	fmt.Fprintf(w, "data: %s\n\n", encoded)
	flusher.Flush()
}
