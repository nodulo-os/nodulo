# API HTTP e eventos SSE

Base local do Compose: `http://localhost:8081`. O servidor aceita CORS da origem definida por `FRONTEND_ORIGIN`. Os exemplos assumem o servidor ativo.

## Autenticação

`POST /api/auth/register` recebe `name`, `email` e `password` (mínimo de 6 caracteres); o cliente nunca escolhe o papel. A política inicial é `approval`: a resposta é `202` com `status: "pending"`, sem token. O acesso só é liberado após análise do administrador. Em `automatic`, o cadastro é aprovado imediatamente apenas se o endereço corresponder a um domínio institucional ou email exato na lista autorizada; a resposta `201` inclui usuário e token. Um endereço fora das regras recebe `403`. `POST /api/auth/login` só emite token para contas aprovadas; pendências e rejeições recebem mensagens apropriadas, e a rejeição inclui a justificativa administrativa. `GET /api/auth/me` devolve o perfil autenticado.

As rotas protegidas exigem `Authorization: Bearer <token>`. Os tokens são HS256, expiram em 24 horas e incluem `user_id`, `email` e `role`. O registro público não permite escolher papel. O endpoint de ingestão RAG exige papel `admin`; prepare a conta inicial com `cmd/seed-admin` conforme [Dados e migrações](06-dados-e-migracoes.md).

```bash
curl -sS http://localhost:8081/api/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"name":"Estudante","email":"estudante@exemplo.edu.br","password":"senha-local-123"}'

curl -sS http://localhost:8081/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"estudante@exemplo.edu.br","password":"senha-local-123"}'

curl -sS http://localhost:8081/api/auth/me \
  -H 'Authorization: Bearer SEU_TOKEN'
```

O painel administrativo configura essa política em `/api/admin/registration/settings` e administra a lista individual em `/api/admin/registration/allowlist`. A decisão de cadastro é feita em `POST /api/admin/users/{id}/review` com `{"status":"approved"}` ou `{"status":"rejected","reason":"..."}`. Toda a família `/api/admin/*` exige JWT de administrador. Configure SMTP com `SMTP_HOST`, `SMTP_PORT`, `SMTP_FROM` e, se necessário, `SMTP_USERNAME`/`SMTP_PASSWORD` para enviar a decisão por email. Se o envio falhar, a decisão permanece registrada e a API indica `notification_sent: false`; configure alertas operacionais para esse caso.

Se uma decisão tiver sido gravada mas o provedor falhar, um administrador pode reenviar a notificação em `POST /api/admin/users/{id}/notification`; o painel também oferece essa ação na lista de utilizadores.

Configure `JWT_SECRET` no ambiente. Se não estiver definido, a API usa `chave-secreta-nodulo-dev` para desenvolvimento local e registra um aviso. Esse valor é público e inseguro: em Coolify ou qualquer instalação compartilhada, defina um segredo aleatório forte e exclusivo. Trocar o segredo invalida todos os tokens emitidos anteriormente.

## Saúde

```http
GET /api/health
```

Retorna `200` com `{"status":"ok"}` se o processo HTTP está ativo. Esse endpoint não verifica inferência nem PostgreSQL em cada chamada.

## Identidade institucional opcional

`GET /api/site-settings` devolve apenas nome, sigla e unidade configurados para exibição pública. Os valores ficam vazios quando a exibição está desligada. Administradores consultam e atualizam a configuração em `GET` e `PATCH /api/admin/site-settings`; todos os campos são opcionais, e a tabela começa desativada e vazia. A aba **Identidade da instalação** no painel configura os mesmos dados.

## Streaming de chat

```http
POST /api/chat
Content-Type: application/json
Accept: text/event-stream
```

Corpo:

```json
{
  "conversation_id": "uuid-opcional",
  "prompt": "Explique uma goroutine em Go",
  "model": "IDENTIFICADOR-DO-MODELO-CONFIGURADO",
  "thinking": true
}
```

`conversation_id`, `model` e `thinking` são opcionais. `thinking: true` pede uma resposta mais analítica; com provedores compatíveis com o parâmetro `think` do protocolo Ollama, também ativa o raciocínio nativo do modelo. Sem essa capacidade, o backend mantém uma instrução de resposta aprofundada. Sem ID, uma conversa é criada e o título usa até os primeiros 40 caracteres do prompt. A mensagem do utilizador é salva antes da chamada ao modelo.

Antes de salvar a nova mensagem, a API recupera até as 10 mensagens anteriores da conversa, em ordem cronológica e verificando a propriedade do usuário. Esse histórico é colocado antes da pergunta atual tanto no fluxo direto do modelo (incluindo imagens) quanto no agente ReAct. Em reenvios/edições, são recuperadas apenas as mensagens anteriores à mensagem editada. A pergunta atual é salva separadamente e não é duplicada no histórico injetado.

Para anexar materiais, envie `multipart/form-data` com `prompt` (opcional quando há arquivos), `conversation_id` (opcional) e um ou mais campos `files`. São aceitos PDF, DOCX, PPTX, TXT e MD. O padrão inicial permite 30 arquivos de até 50 MiB cada, sem limite total configurado; a administração pode ajustar os três valores. Defina um limite como `0` para desativá-lo. O chat exibe cartões com nome, tipo e tamanho; uma migração Go guarda esses metadados para que continuem visíveis ao reabrir a conversa. A API extrai o texto para a solicitação de inferência, mas não guarda os arquivos originais nem o texto extraído. O texto é enviado ao serviço de inferência configurado, portanto reveja as políticas desse serviço antes de anexar informações sensíveis. Desativar limites na aplicação não remove limites próprios do proxy, armazenamento ou contexto do modelo.

```bash
curl -N http://localhost:8081/api/chat \
  -H 'Accept: text/event-stream' \
  -H 'Authorization: Bearer SEU_TOKEN' \
  -F 'prompt=Resuma os conceitos principais deste material' \
  -F 'files=@./material.pdf' \
  -F 'files=@./anotacoes.md'
```

Eventos esperados:

```text
event: meta
data: {"conversation_id":"..."}

event: sources
data: [{"title":"...","source_reference":"..."}]

data: {"text":"fragmento gerado"}

data: [DONE]
```

Ao concluir normalmente, a resposta integral do assistente é gravada antes de `[DONE]`. Se o cliente cancelar a conexão, a API tenta gravar o texto parcial recebido. Clientes devem tratar a resposta como SSE e podem usar `AbortController` para cancelar.

Teste:

```bash
curl -N http://localhost:8081/api/chat \
  -H 'Content-Type: application/json' \
  -H 'Accept: text/event-stream' \
  -H 'Authorization: Bearer SEU_TOKEN' \
  -d '{"prompt":"Explique slices em Go com um exemplo curto."}'
```

## Listar conversas

```http
GET /api/conversations
GET /api/conversations/
```

Retorna um array JSON ordenado por atualização recente, limitado ao usuário autenticado.

```bash
curl -sS http://localhost:8081/api/conversations -H 'Authorization: Bearer SEU_TOKEN'
```

## Ler mensagens

```http
GET /api/conversations/{id}/messages
GET /api/conversations/{id}  # alias legado
```

Retorna as mensagens da conversa em ordem cronológica. Anexos de gráficos gerados incluem `generated: true`, `mime_type` e `data_url` e ficam persistidos no JSONB de anexos da mensagem do assistente (limite agregado de 8 MiB por resposta). IDs inválidos resultam em `400`; conversas ausentes ou pertencentes a outra conta resultam em `404`.

```bash
curl -sS http://localhost:8081/api/conversations/SEU_UUID/messages -H 'Authorization: Bearer SEU_TOKEN'
```

## Ingerir documento para o RAG

```http
POST /api/documents/ingest
Content-Type: application/json
```

Corpo JSON com `title`, `source_reference` e `content`. A API cria chunks com sobreposição, gera embeddings com o modelo configurado em `EMBEDDING_MODEL` e persiste os vetores no PostgreSQL/pgvector. O modelo precisa produzir vetores de 768 dimensões. Retorna `201` com `{"status":"ingested","chunks_count":N}`.

```bash
curl -sS http://localhost:8081/api/documents/ingest \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer TOKEN_DE_ADMIN' \
  -d '{"title":"Apostila de algoritmos","source_reference":"https://repositorio.exemplo.edu.br/algoritmos.pdf, cap. 1","content":"Texto extraído da apostila..."}'
```

O endpoint exige token de usuário com papel `admin`. A role administrativa inicial é criada pelo seed. O painel de administração também permite listar, promover/rebaixar e remover contas; a remoção elimina em cascata conversas e mensagens da conta.

## Painel de administração

Todas as rotas `/api/admin/*` exigem `Authorization: Bearer <token>` de um usuário com papel `admin`.

As configurações de pesquisa web são persistidas no PostgreSQL da instalação:

```http
GET   /api/admin/web-search-settings
PATCH /api/admin/web-search-settings
Content-Type: application/json
```

Exemplo de payload:

```json
{
  "enabled": true,
  "provider": "searxng",
  "endpoint": "http://searxng:8080",
  "result_limit": 5
}
```

`provider` aceita `duckduckgo` ou `searxng`; o endpoint só é necessário para SearXNG. O campo `result_limit` aceita 1–10. Use a interface administrativa para editar essa configuração. A busca pode ser desabilitada sem afetar respostas com o acervo nem o conhecimento geral do modelo.

## Logs e auditoria

A API escreve logs JSON estruturados em `stdout`/`stderr`, visíveis no terminal durante a execução local e no visualizador de logs do Coolify quando implantada. Se o serviço estiver habilitado no Compose, consulte-o com `docker compose logs -f api`. Cada requisição recebe um `X-Request-ID`; o mesmo identificador aparece no log HTTP e nos eventos de auditoria. Os registros HTTP incluem método, caminho sem query string, status, duração, bytes da resposta, endereço remoto e, quando autenticada, o ID do utilizador.

Eventos de auditoria registram resultados de login/cadastro, gerações de chat (modelo, conversa, contagem de caracteres, fontes RAG e duração), mudanças administrativas em contas/modelos/documentos e ciclo de vida dos jobs de ingestão. Senhas, tokens, cabeçalhos de autorização e conteúdo dos prompts/respostas não são gravados. O log é observabilidade operacional e não substitui armazenamento imutável, retenção ou exportação para um SIEM; configure retenção e encaminhamento no ambiente institucional conforme a política de dados.

| Método e rota | Ação |
| --- | --- |
| `GET /api/admin/stats` | Tamanho do banco, tamanho da tabela de embeddings e contagens de usuários, conversas, mensagens e chunks |
| `GET /api/chat/upload-settings` | Limites atuais de anexos para a interface autenticada |
| `GET /api/admin/chat-upload-settings` | Consulta os limites de anexos do chat |
| `PATCH /api/admin/chat-upload-settings` | Atualiza `max_files`, `max_file_size_mib` e `max_total_size_mib`; `0` remove o respectivo limite |
| `GET /api/admin/users` | Lista contas sem expor hashes de senha |
| `PATCH /api/admin/users/{id}` | Altera o papel com corpo `{"role":"admin"}` ou `{"role":"user"}` |
| `DELETE /api/admin/users/{id}` | Remove uma conta e seus dados associados; não permite remover a própria conta nem o último administrador |
| `POST /api/admin/documents/upload-batch` | Aceita arquivos multipart múltiplos e devolve `202` com os jobs criados |
| `POST /api/admin/documents/scrape-url` | Enfileira a captura de uma URL HTTP(S) e sua indexação |
| `GET /api/admin/jobs` | Lista até 200 jobs recentes com status e percentual de chunks processados |
| `GET /api/admin/jobs/stream` | SSE com snapshots dos jobs ativos a cada segundo |
| `POST /api/admin/jobs/{id}/retry` | Retoma um job de arquivo falhado; requer o mesmo arquivo em multipart `file` |
| `GET /api/admin/models` | Lista modelos disponíveis pelo serviço de inferência configurado |
| `POST /api/admin/models/pull` | Recebe `{"model":"nome:tag"}` e transmite eventos SSE `progress`; termina com `[DONE]` |
| `DELETE /api/admin/models` | Recebe `{"model":"nome:tag"}` e remove o modelo local |
| `GET /api/admin/documents` | Lista materiais agrupados por título e referência, com contagem de chunks e última ingestão |
| `DELETE /api/admin/documents` | Recebe `{"source_reference":"..."}` e apaga todos os chunks daquela referência |
| `POST /api/admin/documents/upload` | Recebe multipart com `file` (`.txt` ou `.md`), `title` e `source_reference`; limite de 16 MiB |

O endpoint em lote recebe até 20 arquivos nos campos `files` (ou `file`), com limite combinado de 200 MiB por lote e até 200 MiB por arquivo. Aceita `.pdf`, `.txt`, `.md`, `.zip`, `.docx` e `.pptx`; arquivos Office também são aceitos dentro de ZIP. O campo `title` é opcional e só é aplicado quando há um único arquivo. O ZIP extrai apenas entradas suportadas, mantendo o nome original de cada material. PDFs digitalizados sem camada de texto precisam de OCR antes do envio. O endpoint de URL aceita `url`, `title` e `source_reference`; por segurança, o worker bloqueia endereços locais e de redes privadas.

Se um job de arquivo falhar, o painel oferece **Selecionar arquivo para continuar**. Envie o mesmo arquivo original; a API valida os fragmentos já salvos e gera apenas os que faltam. O endpoint responde `202 Accepted`. Jobs de URL não usam esse endpoint.

```bash
curl -sS -X POST http://localhost:8081/api/admin/documents/upload-batch \
  -H 'Authorization: Bearer TOKEN_DE_ADMIN' \
  -F 'source_reference=ENG0001 · Materiais da disciplina' \
  -F 'files=@livro.pdf' \
  -F 'files=@aulas.zip'

curl -sS -X POST http://localhost:8081/api/admin/documents/scrape-url \
  -H 'Authorization: Bearer TOKEN_DE_ADMIN' \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://exemplo.edu.br/material","title":"Material da disciplina","source_reference":"ENG0001"}'

curl -sS http://localhost:8081/api/admin/jobs \
  -H 'Authorization: Bearer TOKEN_DE_ADMIN'

curl -N http://localhost:8081/api/admin/jobs/stream \
  -H 'Authorization: Bearer TOKEN_DE_ADMIN'
```

O processamento acontece em workers em memória, com dois consumidores e fila limitada a 64 tarefas por instância. O status e o progresso ficam no PostgreSQL; a fila não é distribuída entre réplicas e tarefas ainda na fila são marcadas como falhas no desligamento gracioso.

Exemplo para consultar o painel:

```bash
curl -sS http://localhost:8081/api/admin/stats \
  -H 'Authorization: Bearer TOKEN_DE_ADMIN'

curl -sS http://localhost:8081/api/admin/users \
  -H 'Authorization: Bearer TOKEN_DE_ADMIN'

curl -sS -X PATCH http://localhost:8081/api/admin/users/UUID_DO_USUARIO \
  -H 'Authorization: Bearer TOKEN_DE_ADMIN' \
  -H 'Content-Type: application/json' \
  -d '{"role":"admin"}'
```

As alterações de papel são verificadas no banco em cada rota administrativa e têm efeito imediato, mesmo para tokens emitidos antes da alteração. O sistema impede o rebaixamento ou remoção do último administrador. O utilizador autenticado não pode rebaixar nem remover a própria conta.

## CORS

A API compara o cabeçalho `Origin` com `FRONTEND_ORIGIN` e permite `POST`, `GET`, `DELETE` e `OPTIONS`, com cabeçalhos `Content-Type` e `Authorization`. Para um domínio institucional, configure a origem completa, incluindo esquema, por exemplo `https://ia.exemplo.edu.br`.
