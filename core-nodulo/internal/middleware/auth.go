package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"core-nodulo/internal/observability"
	"core-nodulo/internal/repository"
	"core-nodulo/internal/services"
	"github.com/golang-jwt/jwt/v5"
)

type contextKey struct{}

type AuthenticatedUser struct {
	ID    string `json:"user_id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

func UserFromContext(ctx context.Context) (AuthenticatedUser, bool) {
	user, ok := ctx.Value(contextKey{}).(AuthenticatedUser)
	return user, ok
}

func RequireAuth(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			observability.SetRequestRoute(r.Context(), r.Method, r.Pattern)
			header := r.Header.Get("Authorization")
			parts := strings.Fields(header)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				writeAuthError(w, http.StatusUnauthorized, "Token de autenticação obrigatório.")
				return
			}

			claims := new(services.UserClaims)
			token, err := jwt.ParseWithClaims(parts[1], claims, func(token *jwt.Token) (any, error) {
				return []byte(jwtSecret), nil
			}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer("nodulo-api"), jwt.WithIssuedAt())
			if err != nil || token == nil || !token.Valid || claims.UserID == "" || claims.Subject != claims.UserID || claims.Email == "" || claims.Role == "" || (claims.Role != "admin" && claims.Role != "user") || claims.ExpiresAt == nil {
				writeAuthError(w, http.StatusUnauthorized, "Token inválido ou expirado.")
				return
			}

			user := AuthenticatedUser{ID: claims.UserID, Email: claims.Email, Role: claims.Role}
			observability.SetActor(r.Context(), user.ID)
			ctx := context.WithValue(r.Context(), contextKey{}, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			writeAuthError(w, http.StatusUnauthorized, "Autenticação obrigatória.")
			return
		}
		if user.Role != "admin" {
			writeAuthError(w, http.StatusForbidden, "Acesso restrito a administradores.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireCurrentAdmin checks the persisted role after RequireAuth has
// validated the token, so role changes take effect without waiting for JWT
// expiration.
func RequireCurrentAdmin(users *repository.UserRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := UserFromContext(r.Context())
			if !ok {
				writeAuthError(w, http.StatusUnauthorized, "Autenticação obrigatória.")
				return
			}
			current, err := users.GetUserByID(r.Context(), user.ID)
			if errors.Is(err, repository.ErrNotFound) || (err == nil && current.Role != "admin") {
				writeAuthError(w, http.StatusForbidden, "Acesso restrito a administradores.")
				return
			}
			if err != nil {
				writeAuthError(w, http.StatusInternalServerError, "Não foi possível validar o perfil administrativo.")
				return
			}
			user.Role = current.Role
			ctx := context.WithValue(r.Context(), contextKey{}, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeAuthError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"` + message + `"}` + "\n"))
}
