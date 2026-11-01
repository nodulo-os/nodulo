# Solução de problemas

## `listen tcp ... address already in use`

Outra aplicação já ocupa a porta. O proxy do Coolify pode reservar `8080` no host. No Compose local, a API usa `8081`; altere `API_PORT` no `.env` se também estiver ocupada. Dentro do container, mantenha `PORT=8080`.

## API informa configuração obrigatória ausente

`PORT`, `FRONTEND_ORIGIN`, `INFERENCE_BASE_URL`, `GENERATION_MODEL` e `EMBEDDING_MODEL` são exigidas pela API. Em execução local, confira se `core-nodulo/.env` existe e contém essas variáveis e `DATABASE_URL`. A API carrega o arquivo automaticamente. Variáveis exportadas no shell têm prioridade. `OLLAMA_BASE_URL` e `LLM_MODEL` são aceitas temporariamente como nomes antigos para as configurações de endpoint e geração.

`JWT_SECRET` também é obrigatória e precisa ter pelo menos 32 bytes. Gere uma com `openssl rand -base64 48`; ao trocar a chave, será necessário autenticar novamente todas as contas.

## `relation "users" does not exist`

O banco ainda não recebeu o esquema. Execute `go run ./cmd/migrate` em `core-nodulo` ou consulte [PostgreSQL e migrações Go](06-dados-e-migracoes.md). Não remova volumes de produção para corrigir isso.

## `connection refused` no PostgreSQL

Verifique `docker compose ps`, logs (`docker compose logs postgres`), porta e credenciais. Dentro da rede Compose, o hostname é `postgres`; no host, use `localhost`.

## `connection refused` no serviço de inferência ou modelo não encontrado

Confirme que o endpoint em `INFERENCE_BASE_URL` está acessível a partir do backend e que os identificadores `GENERATION_MODEL` e `EMBEDDING_MODEL` existem nesse serviço. O backend requer atualmente uma API compatível com Ollama; o modelo de embeddings precisa retornar 768 dimensões. Consulte [Configurar o serviço de inferência](11-inferencia.md).

## `unknown model architecture: 'mllama'` ao enviar uma imagem

O backend enviou a solicitação ao serviço configurado, mas o runtime de inferência falhou ao carregar a arquitetura usada por `llama3.2-vision`. Isso não é um erro de formato do arquivo nem de streaming do NÓDULO. O serviço apontado por `INFERENCE_BASE_URL` precisa oferecer suporte a Llama 3.2 Vision/mllama; uma API compatível com Ollama não garante que todas as arquiteturas estejam disponíveis.

Atualize ou substitua o runtime de inferência por uma versão/build que declare suporte a esse modelo, confirme que o modelo visual foi baixado nesse mesmo serviço e teste `/api/tags` e uma chamada `/api/chat` com uma imagem diretamente contra ele. Se não puder atualizar o runtime, configure em `VISION_MODEL` outro modelo de visão que ele suporte. A documentação oficial do [modelo Llama 3.2 Vision no Ollama](https://ollama.com/library/llama3.2-vision) mostra o identificador e o formato de chamada multimodal aceito.

## Chat não conecta em desenvolvimento web

Confira `NEXT_PUBLIC_API_URL` em `web-nodulo/.env.local`, CORS (`FRONTEND_ORIGIN`) e se a API está acessível no navegador. Alterações em `NEXT_PUBLIC_API_URL` exigem reiniciar `npm run dev` ou reconstruir a imagem.

## CORS bloqueia a requisição

`FRONTEND_ORIGIN` precisa corresponder exatamente à origem do navegador (esquema, host e porta), por exemplo `http://localhost:3000`. A configuração atual aceita uma origem.

## Build do Next não passa no healthcheck

A imagem verifica a raiz `/`, pois o frontend ainda não implementa `/api/health`. Verifique também os logs e o endpoint do serviço em `docker compose logs web`.

## Modelo consome muita memória ou fica lento

Modelos maiores exigem mais RAM/VRAM e podem ser lentos em CPU. Escolha um modelo menor compatível com o hardware ou aloque recursos adequados. Suporte GPU depende do driver e da integração container do host; o Compose padrão opera em CPU para maior compatibilidade.
