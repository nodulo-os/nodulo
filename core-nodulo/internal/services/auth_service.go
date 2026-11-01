package services

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"core-nodulo/internal/repository"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials  = errors.New("e-mail ou senha inválidos")
	ErrEmailAlreadyExists  = errors.New("e-mail já cadastrado")
	ErrInvalidUserData     = errors.New("nome ou e-mail inválido")
	ErrPasswordTooShort    = errors.New("a senha deve ter pelo menos 6 caracteres")
	ErrPasswordTooLong     = errors.New("a senha excede o limite suportado")
	ErrEmailNotAuthorized  = errors.New("e-mail não autorizado para cadastro")
	ErrRegistrationPending = errors.New("cadastro aguardando aprovação administrativa")
)

const bcryptMaxPasswordBytes = 72

type AuthService struct {
	users         *repository.UserRepository
	registrations *repository.RegistrationRepository
	jwtSecret     []byte
	now           func() time.Time
}

type RegistrationRejectedError struct{ Reason string }

func (e *RegistrationRejectedError) Error() string {
	if e.Reason == "" {
		return "cadastro rejeitado pela administração"
	}
	return "cadastro rejeitado: " + e.Reason
}

func (e *RegistrationRejectedError) Unwrap() error { return ErrRegistrationRejected }

var ErrRegistrationRejected = errors.New("cadastro rejeitado pela administração")

type UserClaims struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

func NewAuthService(users *repository.UserRepository, registrations *repository.RegistrationRepository, jwtSecret string) *AuthService {
	return &AuthService{users: users, registrations: registrations, jwtSecret: []byte(jwtSecret), now: time.Now}
}

func (s *AuthService) Register(ctx context.Context, name, email, password string) (*repository.User, string, error) {
	name = strings.TrimSpace(name)
	email = normalizeEmail(email)
	parsedEmail, err := mail.ParseAddress(email)
	if name == "" || err != nil || parsedEmail.Address != email {
		return nil, "", ErrInvalidUserData
	}
	if utf8.RuneCountInString(password) < 6 {
		return nil, "", ErrPasswordTooShort
	}
	if len(password) > bcryptMaxPasswordBytes {
		return nil, "", ErrPasswordTooLong
	}

	settings, err := s.registrations.GetSettings(ctx)
	if err != nil {
		return nil, "", err
	}
	status := repository.RegistrationPending
	if settings.Mode == repository.RegistrationModeAutomatic {
		authorized, err := s.registrations.IsEmailAuthorized(ctx, email)
		if err != nil {
			return nil, "", err
		}
		if !authorized {
			return nil, "", ErrEmailNotAuthorized
		}
		status = repository.RegistrationApproved
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, "", fmt.Errorf("hash password: %w", err)
	}
	user, err := s.users.CreateUser(ctx, name, email, string(passwordHash), "user", status)
	if errors.Is(err, repository.ErrEmailAlreadyExists) {
		return nil, "", ErrEmailAlreadyExists
	}
	if err != nil {
		return nil, "", err
	}
	if user.RegistrationStatus != repository.RegistrationApproved {
		return user, "", nil
	}
	token, err := s.issueToken(user)
	if err != nil {
		return nil, "", err
	}
	return user, token, nil
}

func (s *AuthService) Login(ctx context.Context, email, password string) (*repository.User, string, error) {
	user, err := s.users.GetUserByEmail(ctx, normalizeEmail(email))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, "", ErrInvalidCredentials
		}
		return nil, "", err
	}
	if user.PasswordHash == "" || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return nil, "", ErrInvalidCredentials
	}
	if user.RegistrationStatus == repository.RegistrationPending {
		return nil, "", ErrRegistrationPending
	}
	if user.RegistrationStatus == repository.RegistrationRejected {
		return nil, "", &RegistrationRejectedError{Reason: user.RejectionReason}
	}
	if user.RegistrationStatus != repository.RegistrationApproved {
		return nil, "", ErrInvalidCredentials
	}
	token, err := s.issueToken(user)
	if err != nil {
		return nil, "", err
	}
	return user, token, nil
}

func (s *AuthService) issueToken(user *repository.User) (string, error) {
	now := s.now()
	claims := UserClaims{
		UserID: user.ID,
		Email:  user.Email,
		Role:   user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(24 * time.Hour)),
			Issuer:    "nodulo-api",
			Subject:   user.ID,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return "", fmt.Errorf("sign authentication token: %w", err)
	}
	return signed, nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
