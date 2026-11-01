# Início rápido

## Requisitos

- Docker Engine e Docker Compose v2.
- Um endpoint de inferência acessível pela API, compatível com a API Ollama usada pelo backend atual.
- Um modelo de geração e um de embeddings disponíveis no endpoint. O modelo de embeddings deve produzir vetores de 768 dimensões.
- Memória e armazenamento compatíveis com os modelos escolhidos.
- Portas 3000, 5432 e 8081 livres no host, ou portas ajustadas no `.env`.

O Compose não instala nem presume um provedor de inferência. Isso permite usar um serviço local ou remoto e evita iniciar um servidor de modelo que não corresponda à infraestrutura da instalação.

## Subir o projeto

Na raiz do repositório:

```bash
cp .env.example .env
openssl rand -base64 48
```

Edite `.env`, configure `JWT_SECRET` com o primeiro segredo e `POSTGRES_PASSWORD` com o segundo. Preencha também `INFERENCE_BASE_URL`, `GENERATION_MODEL` e `EMBEDDING_MODEL`. O Compose falha fechado se as senhas/segredos obrigatórios estiverem vazios. Para um serviço rodando no host Linux, `host.docker.internal` pode ser usado como hostname se ele estiver acessível a partir da rede Docker; para serviços em Docker, prefira o hostname do container na rede compartilhada.

Inicie a instalação:

```bash
docker compose up --build -d
```

O Compose constrói a API e o frontend, aguarda a saúde do PostgreSQL, executa `cmd/migrate` e só então inicia a API. A aplicação web aguarda o healthcheck da API.

Confira os serviços:

```bash
docker compose ps
docker compose logs -f
```

Abra <http://localhost:3000>. A API responde em <http://localhost:8081> e o healthcheck está em <http://localhost:8081/api/health>.

Em produção no Coolify, configure o domínio e HTTPS no proxy da plataforma e agende os backups do PostgreSQL no painel. Consulte [o guia de implantação](07-implantacao-coolify.md). O perfil `backup` do Compose é uma opção local; ele não instala proxy TLS.

## Criar a primeira conta administrativa

No `.env`, defina `ADMIN_EMAIL`, `ADMIN_NAME` e `ADMIN_PASSWORD` com valores próprios. Em seguida:

```bash
docker compose --profile setup run --rm seed-admin
```

O comando aplica migrações pendentes de forma idempotente e cria ou atualiza essa conta como administradora. Use uma senha exclusiva e forte.

## Parar e preservar os dados

```bash
docker compose down
```

O volume PostgreSQL é preservado. Para apagar definitivamente os dados locais, use `docker compose down -v`; os dados removidos não poderão ser recuperados sem backup.

## Executar API e frontend no host

Para desenvolver sem containers de aplicação, inicie somente o banco:

```bash
docker compose up -d postgres
```

Depois siga o guia [Desenvolvimento local sem Docker para API e frontend](04-desenvolvimento.md). O serviço `db-migrate` do Compose é opcional nesse fluxo: aplique as migrações diretamente com `go run ./cmd/migrate` antes de iniciar a API.

## Diagnóstico rápido

- `INFERENCE_BASE_URL`, `GENERATION_MODEL` ou `EMBEDDING_MODEL` ausentes: confira o `.env` e recrie a configuração do Compose.
- Erro de conexão ao provedor: teste o endpoint a partir do container da API; `localhost` dentro de um container não significa host ou outro serviço.
- Login admin indisponível: execute `docker compose --profile setup run --rm seed-admin` e confira `ADMIN_EMAIL`/`ADMIN_PASSWORD`.
- Migrações: consulte [PostgreSQL e migrações Go](06-dados-e-migracoes.md).
- Mais cenários: [Solução de problemas](08-solucao-de-problemas.md).
