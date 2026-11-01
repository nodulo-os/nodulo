# API do NÓDULO

Backend HTTP em Go para autenticação, conversas, integração de inferência, RAG e administração.

## Executar localmente

Requisitos: Go conforme `go.mod`, PostgreSQL 16 com pgvector e um serviço de inferência compatível com a API Ollama. Copie `core-nodulo/.env.example` para `.env` e preencha `DATABASE_URL`, `JWT_SECRET`, `INFERENCE_BASE_URL`, `GENERATION_MODEL` e `EMBEDDING_MODEL`.

Com o PostgreSQL iniciado:

```bash
go mod download
go run ./cmd/migrate
go run .
```

Para preparar a primeira conta administrativa, defina `ADMIN_EMAIL`, `ADMIN_NAME` e `ADMIN_PASSWORD` no `.env` e execute:

```bash
go run ./cmd/seed-admin
```

As migrações ficam em `pkg/db/migrations.go` e são executadas pelo comando Go; não aplique SQL manualmente. Para a instalação completa via Compose, use as instruções da [raiz do projeto](../README.md).

## Fila durável de ingestão

Os metadados dos jobs e os arquivos enviados ficam persistidos. Os workers reivindicam jobs pendentes diretamente no PostgreSQL com leases; se a API ou o processo cair, outro worker poderá retomá-los quando o lease expirar, continuando do progresso já gravado. O arquivo original é removido do armazenamento após a conclusão.

Defina `INGESTION_STORAGE_DIR` para um diretório persistente e com espaço suficiente. Em execução local, o padrão é `./data/ingestion`. No Docker Compose, a API usa o volume nomeado `ingestiondata`; não remova esse volume ao recriar containers. Em instalações com várias réplicas, configure o mesmo armazenamento de arquivos compartilhado em todas elas. Antes de usar a nova fila, aplique a migração com `go run ./cmd/migrate` (o Compose faz isso pelo serviço `db-migrate`). Jobs antigos sem arquivo ou URL persistidos são marcados como falhos e precisam ser reenviados.

## Estrutura

- `cmd/migrate`: aplica migrações versionadas.
- `cmd/seed-admin`: cria ou atualiza o administrador inicial.
- `internal/handlers`: endpoints HTTP.
- `internal/middleware`: autenticação, autorização e proteções HTTP.
- `internal/repository`: acesso a PostgreSQL.
- `internal/services`: autenticação, inferência, RAG e processamento assíncrono.
- `pkg/db`: pool e migrações.
- `pkg/ollama`: adaptador atual para API compatível com Ollama.
