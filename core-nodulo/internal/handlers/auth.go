package handlers

import (
	"errors"
	"log"
	"net/http"

	"core-nodulo/internal/middleware"
	"core-nodulo/internal/observability"
	"core-nodulo/internal/repository"
	"core-nodulo/internal/services"
)

type AuthHandler struct {
	service  *services.AuthService
	users    *repository.UserRepository
	notifier services.RegistrationNotifier
}

type registerPayload struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginPayload struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authResponse struct {
	User  *repository.User `json:"user"`
	Token string           `json:"token"`
}

func NewAuthHandler(service *services.AuthService, users *repository.UserRepository, notifier services.RegistrationNotifier) *AuthHandler {
	return &AuthHandler{service: service, users: users, notifier: notifier}
}

func (h *AuthHandler) HandleRegister(w http.ResponseWriter, r *http.Request) {
	var payload registerPayload
	if err := decodeAuthPayload(w, r, &payload); err != nil {
		http.Error(w, "Payload JSON inválido.", http.StatusBadRequest)
		return
	}
	user, token, err := h.service.Register(r.Context(), payload.Name, payload.Email, payload.Password)
	if err != nil {
		observability.Audit(r.Context(), "auth.register.rejected")
		writeAuthServiceError(w, err)
		return
	}
	if user.RegistrationStatus == repository.RegistrationPending {
		observability.Audit(r.Context(), "auth.register.pending", "user_id", user.ID)
		message := "Seu cadastro está em análise. Consulte a administração para acompanhar a decisão."
		if notifier, ok := h.notifier.(interface{ Configured() bool }); ok && notifier.Configured() {
			message = "Seu cadastro está em análise. Você receberá um e-mail quando for aprovado ou rejeitado."
		}
		writeJSON(w, http.StatusAccepted, map[string]string{
			"status":  "pending",
			"message": message,
		})
		return
	}
	observability.Audit(r.Context(), "auth.register.succeeded", "user_id", user.ID, "role", user.Role)
	w.Header().Set("X-Registration-Message", "E-mail encontrado na lista de autorizados; acesso liberado automaticamente.")
	writeJSON(w, http.StatusCreated, authResponse{User: user, Token: token})
}

func (h *AuthHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	var payload loginPayload
	if err := decodeAuthPayload(w, r, &payload); err != nil {
		http.Error(w, "Payload JSON inválido.", http.StatusBadRequest)
		return
	}
	user, token, err := h.service.Login(r.Context(), payload.Email, payload.Password)
	if err != nil {
		observability.Audit(r.Context(), "auth.login.rejected")
		writeAuthServiceError(w, err)
		return
	}
	observability.Audit(r.Context(), "auth.login.succeeded", "user_id", user.ID, "role", user.Role)
	writeJSON(w, http.StatusOK, authResponse{User: user, Token: token})
}

func (h *AuthHandler) HandleMe(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.UserFromContext(r.Context())
	if !ok {
		http.Error(w, "Autenticação obrigatória.", http.StatusUnauthorized)
		return
	}
	user, err := h.users.GetUserByID(r.Context(), claims.ID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			http.Error(w, "Usuário não encontrado.", http.StatusUnauthorized)
			return
		}
		log.Printf("load authenticated profile: %v", err)
		http.Error(w, "Não foi possível carregar o perfil.", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func decodeAuthPayload(w http.ResponseWriter, r *http.Request, target any) error {
	return decodeJSONBody(w, r, target, 1<<20)
}

func writeAuthServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, services.ErrInvalidCredentials):
		http.Error(w, "E-mail ou senha inválidos.", http.StatusUnauthorized)
	case errors.Is(err, services.ErrEmailNotAuthorized):
		http.Error(w, "Este e-mail não está na lista de domínios institucionais ou de pessoas autorizadas. Procure um administrador.", http.StatusForbidden)
	case errors.Is(err, services.ErrRegistrationPending):
		http.Error(w, "Seu cadastro está em análise. A decisão será enviada por e-mail se as notificações estiverem configuradas; consulte a administração para acompanhar.", http.StatusForbidden)
	case errors.Is(err, services.ErrRegistrationRejected):
		var rejected *services.RegistrationRejectedError
		if errors.As(err, &rejected) {
			http.Error(w, "Cadastro rejeitado. Justificativa: "+rejected.Reason, http.StatusForbidden)
		} else {
			http.Error(w, "Cadastro rejeitado pela administração.", http.StatusForbidden)
		}
	case errors.Is(err, services.ErrEmailAlreadyExists):
		http.Error(w, "Este e-mail já está cadastrado.", http.StatusConflict)
	case errors.Is(err, services.ErrPasswordTooShort), errors.Is(err, services.ErrPasswordTooLong), errors.Is(err, services.ErrInvalidUserData):
		http.Error(w, err.Error()+".", http.StatusBadRequest)
	default:
		log.Printf("authentication operation failed: %v", err)
		http.Error(w, "Não foi possível concluir a autenticação.", http.StatusInternalServerError)
	}
}
