package main

import (
	"context"
	"log"
	"os"
	"time"

	"core-nodulo/internal/config"
	"core-nodulo/pkg/db"
)

func main() {
	if _, err := config.LoadDotEnv(); err != nil {
		log.Fatalf("carregar arquivo .env: %v", err)
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL é obrigatória para executar as migrações")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := db.NewPostgresPool(ctx, databaseURL)
	if err != nil {
		log.Fatalf("conectar ao PostgreSQL: %v", err)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		log.Fatalf("executar migrações: %v", err)
	}
	log.Print("Migrações do NÓDULO aplicadas com sucesso.")
}
