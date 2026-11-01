package main

import (
	"context"
	"fmt"
	"log"
	"net/mail"
	"os"
	"strings"
	"time"

	"core-nodulo/internal/config"
	"core-nodulo/pkg/db"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	if _, err := config.LoadDotEnv(); err != nil {
		log.Fatalf("carregar arquivo .env: %v", err)
	}
	databaseURL := requiredEnv("DATABASE_URL")
	email := strings.ToLower(strings.TrimSpace(requiredEnv("ADMIN_EMAIL")))
	name := strings.TrimSpace(requiredEnv("ADMIN_NAME"))
	password := requiredEnv("ADMIN_PASSWORD")

	parsedEmail, err := mail.ParseAddress(email)
	if err != nil || parsedEmail.Address != email {
		log.Fatal("ADMIN_EMAIL deve ser um endereço de e-mail válido")
	}
	if name == "" {
		log.Fatal("ADMIN_NAME não pode estar vazio")
	}
	if len(password) < 8 {
		log.Fatal("ADMIN_PASSWORD deve ter pelo menos 8 caracteres")
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("gerar hash da senha: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := db.NewPostgresPool(ctx, databaseURL)
	if err != nil {
		log.Fatalf("conectar ao PostgreSQL: %v", err)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		log.Fatalf("executar migrações pendentes: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, email, name, password_hash, role, created_at)
		VALUES (uuid_generate_v4(), $1, $2, $3, 'admin', CURRENT_TIMESTAMP)
		ON CONFLICT (email) DO UPDATE
		SET name = EXCLUDED.name,
		    password_hash = EXCLUDED.password_hash,
		    role = 'admin',
		    registration_status = 'approved',
		    rejection_reason = NULL`, email, name, string(passwordHash))
	if err != nil {
		log.Fatalf("criar ou atualizar administrador: %v", err)
	}

	fmt.Printf("Administrador preparado: %s\n", email)
}

func requiredEnv(name string) string {
	value, exists := os.LookupEnv(name)
	if !exists || strings.TrimSpace(value) == "" {
		log.Fatalf("%s é obrigatória", name)
	}
	return value
}
