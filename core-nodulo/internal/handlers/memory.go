package handlers

import (
	"errors"
	"log/slog"
	"net/http"

	"core-nodulo/internal/middleware"
	"core-nodulo/internal/observability"
	"core-nodulo/internal/repository"
)

// MemoryHandler exposes user-scoped controls for semantic profile memory.
type MemoryHandler struct {
	repository *repository.UserMemoryRepository
}

func NewMemoryHandler(repo *repository.UserMemoryRepository) *MemoryHandler {
	return &MemoryHandler{repository: repo}
}

type memorySettingsPayload struct {
	Enabled *bool `json:"enabled"`
}

func (h *MemoryHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Autenticação obrigatória.", http.StatusUnauthorized)
		return
	}
	enabled, err := h.repository.MemoryEnabled(r.Context(), user.ID)
	if err != nil {
		h.internalError(w, r, "load semantic memory preference", err)
		return
	}
	facts, err := h.repository.ListFacts(r.Context(), user.ID)
	if err != nil {
		h.internalError(w, r, "list semantic memory facts", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": enabled, "facts": facts})
}

func (h *MemoryHandler) HandleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Autenticação obrigatória.", http.StatusUnauthorized)
		return
	}
	var payload memorySettingsPayload
	if err := decodeJSONBody(w, r, &payload, 1<<20); err != nil || payload.Enabled == nil {
		http.Error(w, "Informe enabled como true ou false.", http.StatusBadRequest)
		return
	}
	if err := h.repository.SetMemoryEnabled(r.Context(), user.ID, *payload.Enabled); err != nil {
		h.internalError(w, r, "update semantic memory preference", err)
		return
	}
	if *payload.Enabled {
		observability.Audit(r.Context(), "user.memory.enabled")
	} else {
		observability.Audit(r.Context(), "user.memory.disabled")
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": *payload.Enabled})
}

func (h *MemoryHandler) HandleDeleteAll(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Autenticação obrigatória.", http.StatusUnauthorized)
		return
	}
	deleted, err := h.repository.DeleteAllFacts(r.Context(), user.ID)
	if err != nil {
		h.internalError(w, r, "delete semantic memory", err)
		return
	}
	observability.Audit(r.Context(), "user.memory.cleared", "deleted_facts", deleted)
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": deleted})
}

func (h *MemoryHandler) HandleDeleteFact(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Autenticação obrigatória.", http.StatusUnauthorized)
		return
	}
	factID := r.PathValue("id")
	if !isUUID(factID) {
		http.Error(w, "Identificador de memória inválido.", http.StatusBadRequest)
		return
	}
	deleted, err := h.repository.DeleteFact(r.Context(), user.ID, factID)
	if err != nil {
		h.internalError(w, r, "delete semantic memory fact", err)
		return
	}
	if !deleted {
		http.Error(w, "Este registro não existe ou não pertence à sua conta.", http.StatusNotFound)
		return
	}
	observability.Audit(r.Context(), "user.memory.fact_deleted", "fact_id", factID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *MemoryHandler) internalError(w http.ResponseWriter, r *http.Request, operation string, err error) {
	if errors.Is(err, r.Context().Err()) {
		return
	}
	slog.ErrorContext(r.Context(), operation, "error", observability.SafeError(err))
	http.Error(w, "Não foi possível atualizar sua memória pessoal agora. Tente novamente.", http.StatusInternalServerError)
}
