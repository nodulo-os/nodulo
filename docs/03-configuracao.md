# Configuração e variáveis de ambiente

## Valores do Compose local

Copie `.env.example` para `.env`. O Compose fornece padrões para opções locais, mas exige `POSTGRES_PASSWORD`, `JWT_SECRET` e as configurações de inferência antes de iniciar.

| Variável | Padrão local | Uso |
| --- | --- | --- |
| `POSTGRES_USER` | `nodulo` | Utilizador inicial do PostgreSQL |
| `POSTGRES_PASSWORD` | Obrigatória | Senha exclusiva gerada para esta instalação; Compose falha se estiver vazia |
| `JWT_SECRET` | Obrigatória no Compose | Gere um segredo aleatório exclusivo; Compose falha se estiver vazio |
| `POSTGRES_DB` | `nodulo` | Banco inicial |
| `POSTGRES_PORT` | `5432` | Porta do PostgreSQL no host |
| `INFERENCE_BASE_URL` | Obrigatória | URL base do serviço de inferência |
| `GENERATION_MODEL` | Obrigatória | Identificador do modelo usado para gerar respostas |
| `EMBEDDING_MODEL` | Obrigatória | Identificador do modelo de embeddings; precisa produzir vetores de 768 dimensões |
| `VISION_MODEL` | Não | Modelo multimodal para anexos de imagem, por exemplo `llama3.2-vision` ou `llava`; deve existir no serviço de inferência |
| `API_PORT` | `8081` | Porta da API no host; internamente é `8080` |
| `WEB_PORT` | `3000` | Porta do frontend no host |
| `FRONTEND_ORIGIN` | `http://localhost:3000` | Origem autorizada pelo CORS da API |
| `NEXT_PUBLIC_API_URL` | `http://localhost:8081/api/chat` | URL pública da API embutida no bundle do Next.js |
| `BACKUP_INTERVAL_SECONDS` | `86400` | Intervalo entre dumps automáticos no perfil `backup` |
| `BACKUP_RETENTION_DAYS` | `14` | Retenção local dos dumps no perfil `backup` |

## Variáveis da API Go

| Variável | Obrigatória | Descrição |
| --- | --- | --- |
| `PORT` | Sim | Porta HTTP dentro do container ou processo (Compose: `8080`) |
| `FRONTEND_ORIGIN` | Sim | Origem exata autorizada pelo CORS |
| `JWT_SECRET` | Sim | Segredo para assinar e validar tokens HS256; mínimo de 32 bytes. Gere com `openssl rand -base64 48` e mantenha-o secreto |
| `TRUSTED_PROXY_CIDRS` | Não | CIDRs dos proxies reversos confiáveis; usado para rate limitar por IP sem confiar em `X-Forwarded-For` enviado diretamente pelo cliente |
| `INFERENCE_BASE_URL` | Sim | URL base do serviço de inferência compatível com a API Ollama usada atualmente pelo backend |
| `GENERATION_MODEL` | Sim | Identificador do modelo de geração disponível nesse serviço |
| `EMBEDDING_MODEL` | Sim | Identificador do modelo de embeddings; deve retornar 768 dimensões para ser compatível com o schema pgvector atual |
| Pesquisa web | Não é variável de ambiente | Ativação, provedor (DuckDuckGo/SearXNG), URL interna SearXNG e quantidade de resultados são configurados pelo painel admin e salvos no PostgreSQL |
| `DATABASE_URL` | Não no host | URI PostgreSQL. O backend tem fallback local para `localhost:5432`; no Compose o serviço injeta endereço interno `postgres` |
| `SMTP_HOST` | Não | Host SMTP do provedor (use o nome DNS do provedor, não o IP); TLS obrigatório |
| `SMTP_PORT` | `587` | Portas `587`/`2587` usam STARTTLS; `465`/`2465` usam TLS implícito |
| `SMTP_USERNAME` / `SMTP_PASSWORD` | Não | Credenciais SMTP, quando exigidas pelo provedor |
| `SMTP_FROM` | Não | Endereço remetente das notificações administrativas |
| `APP_PUBLIC_URL` | Não | URL incluída no email de aprovação/rejeição |

Em Coolify, defina todas as variáveis explicitamente e use valores de segredo fortes. Dentro de containers, `localhost` aponta para o próprio container. Configure URLs internas dos serviços no `DATABASE_URL` e `INFERENCE_BASE_URL`. Configure `TRUSTED_PROXY_CIDRS` com a rede interna real do proxy reverso; não copie o CIDR ilustrativo.

Ao executar `go run .` a partir de `core-nodulo`, a API carrega `core-nodulo/.env` (ou `.env` no diretório atual). Variáveis já exportadas no processo têm precedência sobre o arquivo. No arranque, o log JSON mostra o caminho absoluto do arquivo encontrado, a origem efetiva da URL e do modelo de geração e o host configurado sem credenciais nem caminho. `OLLAMA_BASE_URL` e `LLM_MODEL` ainda são aceitas como aliases de configuração antigos. A imagem Docker não inclui `.env`; em Coolify, cadastre os valores na configuração de ambiente do serviço.

O cliente atual usa endpoints e formato de requisição compatíveis com a API do Ollama (`/api/generate`, `/api/embeddings`, `/api/tags`, entre outros). Por isso, os nomes das configurações são genéricos, mas `INFERENCE_BASE_URL` deve apontar para um serviço compatível. Para integrar provedores com protocolos diferentes, será necessário adicionar um adaptador de protocolo. O modelo de embeddings configurado também precisa gerar vetores de 768 dimensões, conforme a coluna atual do pgvector.

Veja [Configurar o serviço de inferência](11-inferencia.md) para compatibilidade, topologia de rede e implicações de privacidade.

A busca na internet é um recurso separado do serviço de inferência. Configure-a em **Administração → Pesquisa web**; não há URL fixa no código nem variável `.env` necessária. Com SearXNG, a API precisa alcançar a URL configurada pela rede privada. Consulte [RAG e pesquisa web configurável](10-rag.md#pesquisa-web-configurável) para instalação, formatos JSON e implicações de privacidade.

## Proteções da API

A API exige `JWT_SECRET` com pelo menos 32 bytes; gere uma chave exclusiva por instalação com `openssl rand -base64 48`. Ao trocar a chave, todos os tokens existentes deixam de ser válidos e os utilizadores terão de entrar novamente. Não reutilize o valor de exemplo em produção.

Login e cadastro têm limites por endereço de origem (10 e 5 tentativas por minuto, respectivamente). O contador é mantido em memória por instância. Se a API estiver atrás de proxy, configure `TRUSTED_PROXY_CIDRS` com os CIDRs reais dos proxies para que o limite use o IP encaminhado com segurança. Sem essa configuração, o backend ignora `X-Forwarded-For` e usa o endereço da conexão direta.

A API restringe CORS à origem única definida em `FRONTEND_ORIGIN`, valida preflight e envia cabeçalhos de segurança, incluindo `nosniff`, proteção contra enquadramento e política de conteúdo. CORS não substitui autenticação: rotas privadas continuam exigindo JWT e rotas administrativas validam a função atual no banco.

Para o RAG, configure `EMBEDDING_MODEL` com o modelo de embeddings disponível no serviço e compatível com 768 dimensões.

## Variáveis do frontend

`NEXT_PUBLIC_API_URL` é incorporada no JavaScript durante o build. No Compose, a URL padrão acessível pelo navegador é `http://localhost:8081/api/chat`; a aplicação usa essa base também para endpoints de conversas. Em produção, configure a URL pública da API antes de construir a imagem web.

No Coolify, configure seu domínio no serviço web e use o proxy da plataforma para HTTPS. Defina `FRONTEND_ORIGIN`, `APP_PUBLIC_URL` e `NEXT_PUBLIC_API_URL` com os endereços públicos apropriados. O Compose não inclui proxy TLS. O perfil `backup` ativa dumps locais opcionais; em produção, configure backups do banco no painel do Coolify. Consulte [implantação](07-implantacao-coolify.md) e [backup/restauração](06-dados-e-migracoes.md#backup-e-restauração).

O frontend pode ser iniciado localmente com `web-nodulo/.env.local` baseado em `web-nodulo/.env.example`. Nunca coloque segredos em variáveis `NEXT_PUBLIC_*`: elas ficam visíveis no navegador.

## Formato do endereço do banco

O formato usado pela API é:

```text
postgres://UTILIZADOR:SENHA@HOST:PORTA/BANCO?sslmode=disable
```

Caracteres especiais na senha precisam estar codificados para URL. O Compose local usa `sslmode=disable` apenas na rede privada do próprio Compose. Para banco remoto em produção, prefira `sslmode=verify-full`, hostname coberto pelo certificado e a CA confiável disponível no container; não desative a validação TLS para contornar erros de certificado.

## Configuração por instituição

Cada instalação pode substituir modelo, domínio, credenciais, limites e serviços. Antes de uso multiusuário, planeje autenticação institucional (por exemplo, OIDC/SAML), autorização por utilizador, isolamento entre instituições, gestão de dados, logs sem informação sensível, backups, monitoramento e política de retenção. Essas capacidades não estão implementadas no estado atual.

O nome da organização, a sigla e a unidade exibidos no app não são variáveis nem valores de marca fixos. São opcionais, ficam na tabela `institutional_settings` e podem ser editados na aba **Identidade da instalação** do painel admin. A exibição começa desligada; deixar os campos vazios mantém a interface neutra para qualquer universidade, escola ou comunidade.

O modo automático compara o domínio declarado no email com a lista que o administrador configurou; ele não confirma, por si só, que a pessoa controla aquela caixa postal. Para uma garantia forte de identidade institucional, use o modo de aprovação administrativa ou integre o provedor institucional de identidade (OIDC/SAML) e verificação de email antes de habilitar o uso aberto por domínio.
