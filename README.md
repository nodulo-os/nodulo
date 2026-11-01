<div align="center">
  <h1>NÓDULO</h1>
  <p><strong>Inferência configurável · Dados sob controle da instalação</strong></p>

  <p>
    <img alt="License pending" src="https://img.shields.io/badge/License-pending%20maintainer%20decision-orange?style=flat-square">
    <img alt="Go" src="https://img.shields.io/badge/API-Go-00ADD8?style=flat-square&logo=go">
    <img alt="Next.js" src="https://img.shields.io/badge/Web-Next.js-111111?style=flat-square&logo=next.js">
    <img alt="PostgreSQL" src="https://img.shields.io/badge/Data-PostgreSQL-4169E1?style=flat-square&logo=postgresql">
    <img alt="Docker Compose" src="https://img.shields.io/badge/Setup-Docker%20Compose-2496ED?style=flat-square&logo=docker">
  </p>
</div>

O **NÓDULO** é uma plataforma de inteligência artificial em preparação para publicação comunitária, voltada a universidades, escolas, empresas e pessoas que valorizam autonomia digital. A instalação escolhe onde executar a aplicação, qual serviço de inferência conectar, quem pode acessar e como administrar os dados. A licença ainda precisa ser escolhida pelos mantenedores; até lá, o código não deve ser redistribuído como software open source licenciado.

O projeto não vende nem monetiza conversas ou documentos. A privacidade efetiva também depende das escolhas de implantação: quando a inferência usa um serviço externo, revise as políticas desse serviço e os dados enviados a ele.

## Começar com Docker Compose

O Compose inicia PostgreSQL, aplica as migrações Go e sobe a API e a interface. Antes do primeiro arranque, configure o endpoint de inferência e os modelos da instalação.

### 1. Preparar a configuração

Na raiz do repositório:

```bash
cp .env.example .env
openssl rand -base64 48
openssl rand -hex 32
```

Edite `.env`, coloque o primeiro segredo em `JWT_SECRET`, o segundo em `POSTGRES_PASSWORD`, além de:

```dotenv
INFERENCE_BASE_URL=https://ENDERECO-DO-SERVICO-COMPATIVEL
GENERATION_MODEL=IDENTIFICADOR-DO-MODELO-DE-GERACAO
EMBEDDING_MODEL=IDENTIFICADOR-DO-MODELO-DE-EMBEDDINGS
```

O backend usa atualmente o protocolo compatível com a API do Ollama. Para RAG, o modelo de embeddings deve retornar vetores de **768 dimensões**. Se o serviço de inferência estiver em outro container, use o hostname da rede Docker; `localhost` dentro da API aponta para o próprio container.

### 2. Construir e iniciar

```bash
docker compose up --build -d
```

O serviço `db-migrate` aguarda o PostgreSQL saudável e aplica as migrações antes da API iniciar. O frontend inicia quando a API passa no healthcheck.

- Interface: <http://localhost:3000>
- API: <http://localhost:8081>
- Saúde da API: <http://localhost:8081/api/health>

#### Domínio no Coolify e backups automáticos

No Coolify, associe seu domínio ao serviço web e use o proxy gerenciado pela plataforma para servir o site por HTTPS. Configure `FRONTEND_ORIGIN`, `APP_PUBLIC_URL` e `NEXT_PUBLIC_API_URL` com os endereços públicos corretos. O Compose não inicia um proxy TLS.

```bash
docker compose --profile backup up --build -d
```

O serviço de backup do Compose produz dumps diários e mantém 14 dias no volume local. Como você usa Coolify, prefira agendar os backups do PostgreSQL no painel e enviá-los a um destino S3 externo. Consulte [operação no Coolify](docs/07-implantacao-coolify.md) e [backup/restauração](docs/06-dados-e-migracoes.md#backup-e-restauração).

### 3. Criar o primeiro administrador

Defina `ADMIN_EMAIL`, `ADMIN_NAME` e `ADMIN_PASSWORD` em `.env` e execute:

```bash
docker compose --profile setup run --rm seed-admin
```

Abra `/register` para cadastrar outras contas. Em instalações novas, os cadastros aguardam aprovação administrativa; configure a política de acesso no painel.

### Comandos úteis

```bash
docker compose ps                 # estado e healthchecks
docker compose logs -f            # acompanhar logs
docker compose logs -f api        # logs da API
docker compose down               # parar, preservando o banco
docker compose down -v            # apagar permanentemente os volumes e os dados
```

Compose falha ao iniciar se `POSTGRES_PASSWORD` ou `JWT_SECRET` não estiverem configurados. Use segredos aleatórios diferentes e não os inclua no repositório.

## Executar sem Docker

É possível executar PostgreSQL, API e frontend separadamente. Os guias explicam pré-requisitos, variáveis, migrações, criação do administrador, comandos de execução e solução de problemas:

- [Execução rápida e Docker Compose](docs/02-inicio-rapido.md)
- [Configuração e variáveis de ambiente](docs/03-configuracao.md)
- [Desenvolvimento local sem Docker para API e frontend](docs/04-desenvolvimento.md)
- [PostgreSQL e migrações Go](docs/06-dados-e-migracoes.md)

## Princípios

- **Dados sob controle da instalação:** cada organização escolhe infraestrutura, serviço de inferência e regras de acesso.
- **Sem exploração comercial dos dados:** o NÓDULO não tem como objetivo vender nem monetizar conversas ou documentos.
- **Flexível por projeto:** a mesma base pode ser adaptada a ambientes acadêmicos, empresariais e pessoais.
- **Conhecimento recuperável:** o RAG busca trechos num acervo PostgreSQL/pgvector e complementa lacunas com o modelo de geração.
- **Pesquisa web opcional:** cada instalação escolhe no painel se usa DuckDuckGo, um SearXNG próprio ou mantém a consulta externa desativada.

## Componentes

| Diretório | Responsabilidade |
| --- | --- |
| [`core-nodulo/`](core-nodulo/) | API Go, autenticação, autorização, persistência, RAG e fila de ingestão |
| [`web-nodulo/`](web-nodulo/) | Interface Next.js App Router, chat e painel administrativo |
| [`docs/`](docs/) | Guias de instalação, operação, arquitetura, API, migrações e contribuição |
| [`academic/`](academic/) | Proposta e materiais de fundamentação do projeto |
| [`docker-compose.yml`](docker-compose.yml) | Ambiente de desenvolvimento com migrações automáticas |

## Documentação

Comece pelo [índice da documentação](docs/README.md). Há percursos para quem quer executar, desenvolver, ensinar, administrar, implantar com Coolify, [configurar o executor Python privado](docs/deploy-python-executor-coolify.md), [configurar o RAG e a pesquisa web](docs/10-rag.md#pesquisa-web-configurável) ou [preparar uma publicação no GitHub](docs/12-publicacao-github.md).

## Contribuir

Leia [como contribuir](docs/09-contribuicao.md), descreva o ambiente ao relatar um problema e nunca publique arquivos `.env`, credenciais, dados pessoais ou documentos privados. O repositório ainda precisa da escolha e inclusão de uma licença antes de uma publicação formal como software open source.

## Licença

**Pendente de definição pelos mantenedores.** Não há arquivo `LICENSE`; consulte [a lista de decisões de publicação](docs/12-publicacao-github.md) antes de distribuir ou anunciar o repositório como open source.
