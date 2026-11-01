package handlers

import (
	"log/slog"
	"net/http"

	"core-nodulo/internal/middleware"
	"core-nodulo/internal/observability"
	"core-nodulo/internal/repository"
)

type ChatUploadSettingsHandler struct {
	Repository *repository.ChatUploadSettingsRepository
}

func NewChatUploadSettingsHandler(repo *repository.ChatUploadSettingsRepository) *ChatUploadSettingsHandler {
	return &ChatUploadSettingsHandler{Repository: repo}
}

func (h *ChatUploadSettingsHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
	settings, err := h.Repository.Get(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "load chat upload settings", "error", observability.SafeError(err))
		writeAdminError(w, http.StatusInternalServerError, "Não foi possível consultar os limites de anexos.")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (h *ChatUploadSettingsHandler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	var settings repository.ChatUploadSettings
	if err := decodeAdminJSON(w, r, &settings); err != nil {
		writeAdminError(w, http.StatusBadRequest, "Configuração de anexos inválida.")
		return
	}
	const maxInt64Value int64 = 1<<63 - 1
	maxSafeMiB := (maxInt64Value - int64(multipartOverheadBytes)) / (1 << 20)
	if settings.MaxFiles < 0 || int64(settings.MaxFiles) > 1<<31-1 ||
		settings.MaxFileSizeMiB < 0 || settings.MaxFileSizeMiB > maxSafeMiB ||
		settings.MaxTotalSizeMiB < 0 || settings.MaxTotalSizeMiB > maxSafeMiB {
		writeAdminError(w, http.StatusBadRequest, "Use zero para remover um limite ou informe um valor positivo válido.")
		return
	}
	actor, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeAdminError(w, http.StatusUnauthorized, "Autenticação obrigatória.")
		return
	}
	updated, err := h.Repository.Update(r.Context(), settings, actor.ID)
	if err != nil {
		slog.ErrorContext(r.Context(), "save chat upload settings", "error", observability.SafeError(err))
		writeAdminError(w, http.StatusInternalServerError, "Não foi possível salvar os limites de anexos.")
		return
	}
	observability.Audit(r.Context(), "admin.chat_upload_settings.updated",
		"actor_user_id", actor.ID,
		"max_files", updated.MaxFiles,
		"max_file_size_mib", updated.MaxFileSizeMiB,
		"max_total_size_mib", updated.MaxTotalSizeMiB,
	)
	writeJSON(w, http.StatusOK, updated)
}
