# PostgreSQL, esquema e migrações

## Modelo atual

- `users`: e-mail único, nome, hash de senha e papel (`user`/`admin`). Senhas nunca são armazenadas em texto puro. A migração 005 também corrige instalações antigas onde a tabela já existia sem unicidade no e-mail; ela para com erro claro se encontrar duplicatas, sem apagar nem combinar contas automaticamente.
- `conversations`: utilizador proprietário, título e datas de criação/atualização.
- `messages`: conversa, papel (`user`/`assistant`), conteúdo e data de criação.
- `document_chunks`: texto institucional, referência, metadados e embedding `vector(768)` para busca por cosseno.
- `ingestion_jobs`: estado e progresso de ingestões (`pending`, `processing`, `completed`, `failed`), tipo de origem (`pdf`, `zip`, `url`, `text`, `docx`, `pptx`), título e contagens de chunks. Criada pela migração Go 006, ampliada pela 007 e pela 012, com índice por estado e data de criação.
- `registration_settings`, `registration_allowed_domains` e `registration_allowlist`: política institucional de cadastro e autorizações por domínio/email. A migração Go 008 adiciona também status de cadastro (`pending`, `approved`, `rejected`), justificativa de rejeição e dados de revisão em `users`.
- `institutional_settings`: nome, sigla e unidade opcionais para identificar a instituição. A migração Go 009 cria a configuração desativada e sem valores por padrão.
- `web_search_settings`: habilitação, provedor (DuckDuckGo/SearXNG), endpoint privado e quantidade de resultados da pesquisa web. A migração Go 016 inicializa a seleção compatível com o fallback atual; administradores podem desligar ou alternar a integração pelo painel.

O utilizador acadêmico compartilhado continua criado pela migração base para compatibilidade com o chat local. O seed administrativo é um comando separado, executado explicitamente.

Migrações existentes mantêm as contas já criadas como `approved`. Para instalações novas, a política começa como `approval`, evitando liberar cadastros antes da configuração explícita de um administrador. Para liberar cadastros por email institucional, o administrador configura os domínios e/ou a lista individual no painel. A lista afeta novos cadastros; remover um email ou domínio não revoga contas já aprovadas.

## Como funcionam as migrações

O esquema é mantido em migrações Go versionadas em [pkg/db/migrations.go](../core-nodulo/pkg/db/migrations.go). `db.Migrate` adquire um advisory lock PostgreSQL, aplica cada versão numa transação e registra versão/nome/data em `schema_migrations`. Execuções repetidas não reaplicam versões concluídas. A aplicação interrompe se encontrar uma versão desconhecida ou com nome divergente.

Não há inicialização de tabelas por scripts do entrypoint PostgreSQL nem comandos manuais de DDL no Compose. O serviço `db-migrate` executa `cmd/migrate` e a API só inicia após sucesso.

## Executar migrações manualmente

Com `DATABASE_URL` preenchida no `core-nodulo/.env`, execute na pasta `core-nodulo`:

```bash
go run ./cmd/migrate
```

O comando é seguro para rodar novamente. Em implantação Coolify, configure o mesmo comando como etapa pré-deploy/one-off com `DATABASE_URL` do serviço PostgreSQL privado. Faça backup antes de aplicar mudanças em dados existentes.

## Seed do administrador

Depois das migrações, configure `ADMIN_EMAIL`, `ADMIN_NAME` e `ADMIN_PASSWORD` no `core-nodulo/.env` e execute:

```bash
cd core-nodulo
go run ./cmd/seed-admin
```

Use uma senha forte e exclusiva. O comando aplica migrações pendentes, gera bcrypt e faz upsert pelo e-mail; repetir o seed atualiza nome, senha e papel desse usuário para `admin`. Variáveis exportadas pelo shell substituem os valores do `.env`.

## Compose local

`docker compose up --build -d` inicia PostgreSQL, espera o healthcheck do banco, executa `cmd/migrate` e inicia API/web depois da migração. O Compose não inicia um servidor de inferência; configure `INFERENCE_BASE_URL`, `GENERATION_MODEL` e `EMBEDDING_MODEL` no `.env` antes.

Para executar somente a migração via Compose:

```bash
docker compose run --rm db-migrate
```

Para criar o administrador através do serviço de setup:

```bash
docker compose --profile setup run --rm seed-admin
```

Defina `ADMIN_EMAIL`, `ADMIN_NAME` e `ADMIN_PASSWORD` no `.env` antes de executar o seed. As migrações são aplicadas automaticamente também pelo comando do seed.

## Backup e restauração

O Compose inclui um serviço opt-in que cria dumps custom-format diários e conserva 14 dias no volume `postgresbackups`:

```bash
docker compose --profile backup up -d postgres-backup
docker compose logs -f postgres-backup
```

Configure `BACKUP_INTERVAL_SECONDS` e `BACKUP_RETENTION_DAYS` no `.env`. Para copiar um dump ao host antes de enviá-lo a um destino externo:

```bash
mkdir -p backups
docker compose cp postgres-backup:/backups/NOME-DO-DUMP.dump ./backups/
```

O destino de produção deve ser externo ao servidor/volume, protegido por criptografia e credenciais próprias. O serviço Compose não faz envio externo nem criptografa os arquivos; um volume no mesmo host não protege contra perda do servidor, comprometimento ou exclusão dos volumes.

```bash
docker compose exec -T postgres sh -c 'pg_restore --clean --if-exists --no-owner --no-acl -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < ./backups/NOME-DO-DUMP.dump
```

O exemplo de restauração exige um dump copiado ao host e deve ser usado numa base de restauração ou numa janela de manutenção, após validar o nome do arquivo e a instalação de destino. Faça um backup da base atual antes. Backup e restauração não alteram o código de migrações; após restaurar, aplique as migrações com `cmd/migrate` conforme a versão da aplicação. Teste periodicamente a restauração numa instância isolada.

## Criar uma migração nova

1. Acrescente a próxima versão monotônica em `pkg/db/migrations.go` e dê um nome estável.
2. Implemente a função `up` com operações idempotentes adequadas para instalações já existentes.
3. Adicione a entrada ao registro `migrations`.
4. Revise compatibilidade com versões da API que podem rodar durante o deploy, faça backup e aplique em staging primeiro.
5. Nunca edite migrações já aplicadas; correções são uma nova versão.

As migrações atuais usam SQL como instruções executadas por funções Go/pgx, para aproveitar os recursos próprios do PostgreSQL sem depender de scripts `psql` ou execução manual. Mudanças de esquema e sua versão são controladas pelo runner Go.
