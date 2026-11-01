package handlers

import (
	"log"
	"net/http"
	"strings"
	"unicode/utf8"

	"core-nodulo/internal/middleware"
	"core-nodulo/internal/observability"
	"core-nodulo/internal/repository"
)

type InstitutionalSettingsHandler struct {
	Repository *repository.InstitutionalSettingsRepository
}

func NewInstitutionalSettingsHandler(repo *repository.InstitutionalSettingsRepository) *InstitutionalSettingsHandler {
	return &InstitutionalSettingsHandler{Repository: repo}
}

func (h *InstitutionalSettingsHandler) HandleGetPublic(w http.ResponseWriter, r *http.Request) {
	settings, err := h.Repository.Get(r.Context())
	if err != nil {
		log.Printf("load public institutional settings: %s", observability.SafeError(err))
		writeAdminError(w, http.StatusInternalServerError, "Não foi possível consultar as informações públicas da instalação.")
		return
	}
	if !settings.ShowInstitution {
		settings.InstitutionName = ""
		settings.InstitutionAcronym = ""
		settings.InstitutionUnit = ""
	}
	writeJSON(w, http.StatusOK, settings)
}

func (h *InstitutionalSettingsHandler) HandleGetAdmin(w http.ResponseWriter, r *http.Request) {
	settings, err := h.Repository.Get(r.Context())
	if err != nil {
		log.Printf("load institutional settings: %s", observability.SafeError(err))
		writeAdminError(w, http.StatusInternalServerError, "Não foi possível consultar as informações da instalação.")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (h *InstitutionalSettingsHandler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	var settings repository.InstitutionalSettings
	if err := decodeAdminJSON(w, r, &settings); err != nil {
		writeAdminError(w, http.StatusBadRequest, "Payload JSON inválido.")
		return
	}
	settings.InstitutionName = strings.TrimSpace(settings.InstitutionName)
	settings.InstitutionAcronym = strings.TrimSpace(settings.InstitutionAcronym)
	settings.InstitutionUnit = strings.TrimSpace(settings.InstitutionUnit)
	if utf8.RuneCountInString(settings.InstitutionName) > 160 ||
		utf8.RuneCountInString(settings.InstitutionAcronym) > 32 ||
		utf8.RuneCountInString(settings.InstitutionUnit) > 120 {
		writeAdminError(w, http.StatusBadRequest, "Nome, sigla ou unidade excedem o limite permitido.")
		return
	}
	if settings.ShowInstitution && settings.InstitutionName == "" {
		writeAdminError(w, http.StatusBadRequest, "Informe o nome da instituição ou desative a exibição institucional.")
		return
	}
	actor, _ := middleware.UserFromContext(r.Context())
	updated, err := h.Repository.Update(r.Context(), settings, actor.ID)
	if err != nil {
		log.Printf("save institutional settings: %s", observability.SafeError(err))
		writeAdminError(w, http.StatusInternalServerError, "Não foi possível salvar as informações da instalação.")
		return
	}
	observability.Audit(r.Context(), "admin.institutional_settings.updated", "actor_user_id", actor.ID, "show_institution", updated.ShowInstitution)
	writeJSON(w, http.StatusOK, updated)
}
