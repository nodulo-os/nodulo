# Desenvolvimento local sem Docker para API e frontend

Este percurso executa Go e Next.js no host e usa Docker somente para PostgreSQL. O endpoint de inferência pode ser local ou remoto, desde que esteja acessível pela API e seja compatível com o protocolo de inferência usado pelo backend.

## Ferramentas

- Go 1.26 ou superior, conforme `core-nodulo/go.mod`.
- Node.js 20 e npm.
- Docker Engine e Docker Compose v2 para PostgreSQL 16/pgvector.
- Acesso ao endpoint configurado em `INFERENCE_BASE_URL` e aos modelos configurados em `GENERATION_MODEL` e `EMBEDDING_MODEL`.

## 1. Clonar e configurar

Na raiz do projeto, copie os exemplos somente se os arquivos locais ainda não existirem:

```bash
cp .env.example .env
cp core-nodulo/.env.example core-nodulo/.env
cp web-nodulo/.env.example web-nodulo/.env.local
```

Gere um segredo para `JWT_SECRET` com `openssl rand -base64 48`. No arquivo `core-nodulo/.env`, configure esse segredo, o endpoint/modelos de inferência, `FRONTEND_ORIGIN=http://localhost:3000` e uma `DATABASE_URL` que corresponda às credenciais do PostgreSQL.

Para o `.env` da raiz, use a mesma senha em `POSTGRES_PASSWORD`. As portas do exemplo são PostgreSQL `5432`, API `8081` e frontend `3000`.

Em `web-nodulo/.env.local`, use `NEXT_PUBLIC_API_URL=http://localhost:8081/api/chat`.

## 2. Iniciar PostgreSQL

Na raiz:

```bash
docker compose up -d postgres
docker compose ps postgres
```

O banco usa o volume `pgdata`; parar o container não remove os dados.

## 3. Instalar dependências e migrar

```bash
cd core-nodulo
go mod download
go run ./cmd/migrate
```

As migrações ficam em `pkg/db/migrations.go`, são versionadas e transacionais. O comando pode ser executado novamente com segurança.

## 4. Criar administrador

Defina `ADMIN_EMAIL`, `ADMIN_NAME` e `ADMIN_PASSWORD` no `core-nodulo/.env`, com uma senha exclusiva de pelo menos oito caracteres. Execute:

```bash
go run ./cmd/seed-admin
```

Esse comando aplica também qualquer migração pendente. Pode ser repetido: o usuário correspondente ao email é atualizado como administrador.

## 5. Iniciar a API

Ainda em `core-nodulo`:

```bash
go run .
```

A API lê `.env` automaticamente. Confirme `http://localhost:8081/api/health`. Se a porta padrão do exemplo for diferente, ajuste `PORT` e `NEXT_PUBLIC_API_URL` de acordo.

## 6. Iniciar o frontend

Em outro terminal, na raiz:

```bash
cd web-nodulo
npm ci
npm run dev
```

Abra <http://localhost:3000>. `NEXT_PUBLIC_API_URL` é lida no build/início do Next.js; reinicie o servidor após alterá-la.

## Execução inteiramente sem Docker

Instale PostgreSQL 16 com a extensão pgvector e crie um banco e utilizador para a aplicação conforme a política do seu sistema. Coloque a conexão em `DATABASE_URL` no `core-nodulo/.env`, depois execute os passos 3 a 6. As migrações Go criam as extensões, tabelas e índices necessários; não aplique DDL manualmente.

## Mapa do código

```text
core-nodulo/
  main.go                         configuração HTTP, CORS, dependências e rotas
  internal/handlers/              endpoints HTTP, autenticação e streaming SSE
  internal/middleware/            autenticação, autorização e segurança HTTP
  internal/repository/            consultas e transações PostgreSQL
  internal/services/              autenticação, RAG, email e worker de ingestão
  pkg/db/migrations.go            migrações Go versionadas
  pkg/ollama/client.go            adaptador atual compatível com API Ollama
  cmd/migrate/                    runner de migrações para deploy/CI
  cmd/seed-admin/                 criação idempotente do administrador
web-nodulo/
  app/                            App Router, páginas e estilos
  components/                     interface e componentes
  context/                        sessão e preferência de tema
  hooks/                          estado e streaming do chat
```

## Verificações de desenvolvimento

```bash
cd core-nodulo && gofmt -w . && go build ./...
cd ../web-nodulo && npm run lint && npm run build
```

Não inclua arquivos `.env`, tokens, senhas reais, dumps do banco ou documentos identificáveis em commits.
