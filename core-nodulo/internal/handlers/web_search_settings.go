package handlers

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"core-nodulo/internal/middleware"
	"core-nodulo/internal/observability"
	"core-nodulo/internal/repository"
)

type WebSearchSettingsHandler struct {
	Repository *repository.WebSearchSettingsRepository
}

func NewWebSearchSettingsHandler(repo *repository.WebSearchSettingsRepository) *WebSearchSettingsHandler {
	return &WebSearchSettingsHandler{Repository: repo}
}

func (h *WebSearchSettingsHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
	settings, err := h.Repository.Get(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "load web search settings", "error", observability.SafeError(err))
		writeAdminError(w, http.StatusInternalServerError, "Não foi possível carregar as configurações de pesquisa web.")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (h *WebSearchSettingsHandler) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	var settings repository.WebSearchSettings
	if err := decodeAdminJSON(w, r, &settings); err != nil {
		writeAdminError(w, http.StatusBadRequest, "Configuração JSON inválida.")
		return
	}
	settings.Provider = strings.ToLower(strings.TrimSpace(settings.Provider))
	settings.Endpoint = strings.TrimRight(strings.TrimSpace(settings.Endpoint), "/")
	if settings.Provider != repository.WebSearchProviderDuckDuckGo && settings.Provider != repository.WebSearchProviderSearXNG {
		writeAdminError(w, http.StatusBadRequest, "Escolha DuckDuckGo ou SearXNG como provedor.")
		return
	}
	if settings.ResultLimit < 1 || settings.ResultLimit > 10 {
		writeAdminError(w, http.StatusBadRequest, "A quantidade de resultados deve ficar entre 1 e 10.")
		return
	}
	if settings.Provider == repository.WebSearchProviderSearXNG && settings.Enabled {
		endpoint, err := url.Parse(settings.Endpoint)
		if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || len(settings.Endpoint) > 2048 {
			writeAdminError(w, http.StatusBadRequest, "Informe a URL base válida da instância SearXNG, sem credenciais, query ou fragmento. Ela deve permitir respostas JSON.")
			return
		}
	}
	actor, _ := middleware.UserFromContext(r.Context())
	updated, err := h.Repository.Update(r.Context(), settings, actor.ID)
	if err != nil {
		slog.ErrorContext(r.Context(), "save web search settings", "error", observability.SafeError(err))
		writeAdminError(w, http.StatusInternalServerError, "Não foi possível salvar as configurações de pesquisa web.")
		return
	}
	observability.Audit(r.Context(), "admin.web_search_settings.updated",
		"actor_user_id", actor.ID,
		"enabled", updated.Enabled,
		"provider", updated.Provider,
		"result_limit", updated.ResultLimit,
	)
	writeJSON(w, http.StatusOK, updated)
}
