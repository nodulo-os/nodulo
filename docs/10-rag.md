# RAG e conhecimento acadêmico

## Fluxo

1. Uma pessoa administradora envia o texto extraído de uma apostila ou fonte institucional a `POST /api/documents/ingest`.
2. A API divide o texto em blocos de aproximadamente 700 caracteres, buscando limites de palavra e sobrepondo 100 caracteres entre blocos.
3. O modelo definido em `EMBEDDING_MODEL` produz um vetor de 768 dimensões para cada bloco.
4. PostgreSQL com pgvector guarda o texto e os vetores; um índice HNSW acelera a busca por distância de cosseno.
5. Antes da geração, a API tenta embeddar a pergunta, recupera até três trechos e usa os que excedam similaridade 0,4.
6. Quando há fontes relevantes, a API emite `event: sources` no SSE e as usa como referência factual, complementando lacunas com o conhecimento geral do modelo. Sem resultados relevantes, ou se a recuperação/embedding falhar, a pergunta segue para geração com o conhecimento geral do modelo; a ausência de documentos não causa recusa nem bloqueia a resposta.
7. Se a similaridade do acervo não superar `0.4`, ou a pergunta pedir informação recente (por exemplo, “notícias”, “hoje” ou “atual”), a API pode iniciar pesquisa web iterativa, se habilitada no painel. O agente pode fazer até quatro ações entre pesquisar e abrir páginas, registrar observações e então sintetizar uma resposta com citações. O fluxo é limitado por timeout e páginas externas são tratadas como conteúdo não confiável.

## Pesquisa web configurável

A pesquisa web é opcional e pode ser desligada ou alternada em **Administração → Pesquisa web**. A configuração fica no PostgreSQL da instalação (`web_search_settings`), portanto persiste após reinícios e é aplicada nos próximos chats sem reiniciar a API. O limite de resultados é ajustável de 1 a 10.

Opções disponíveis:

- **DuckDuckGo HTML:** não exige serviço adicional. Quando acionado, o texto da pergunta atual é enviado ao DuckDuckGo pelo servidor NÓDULO. Histórico e anexos não são enviados ao buscador. Avalie se a própria pergunta pode conter dados sensíveis antes de ativar.
- **SearXNG auto-hospedado:** informe no painel a URL base alcançável pelo container da API (por exemplo, `http://searxng:8080`). O NÓDULO chama `/search?format=json`; habilite `json` em `search.formats` no `settings.yml` do SearXNG. Instâncias públicas podem desabilitar esse formato. A documentação oficial recomenda usar o template Compose mantido pelo projeto SearXNG e manter a instância atualizada: [instalação oficial em containers](https://docs.searxng.org/admin/installation-docker.html).

Para começar com o Compose oficial do SearXNG:

```bash
mkdir -p searxng && cd searxng
curl -fsSLO https://raw.githubusercontent.com/searxng/searxng/master/container/docker-compose.yml
curl -fsSLO https://raw.githubusercontent.com/searxng/searxng/master/container/.env.example
cp .env.example .env
# Edite .env e os arquivos em core-config/ conforme a documentação oficial.
docker compose up -d
```

Na configuração SearXNG, habilite explicitamente a resposta JSON, por exemplo dentro da chave `search`:

```yaml
search:
  formats:
    - html
    - json
```

Depois conecte o SearXNG à mesma rede Docker da API (ou configure uma rota privada alcançável pela API), abra **Administração → Pesquisa web**, escolha **SearXNG auto-hospedado**, informe a URL base e salve. Em Coolify, use o hostname interno/rede privada entre os serviços; não use `localhost` para apontar para outro container.

SearXNG evita que o navegador do usuário faça diretamente a busca e expõe aos motores a conexão de saída do servidor SearXNG. Isso não torna consultas anônimas por garantia: os motores selecionados ainda recebem a consulta e podem aplicar seus próprios registros, e o endereço de saída da infraestrutura SearXNG continua visível para eles. Para menor exposição, escolha motores e políticas de retenção adequados, limite o acesso à instância e não coloque dados pessoais/sigilosos na consulta.

O endpoint administrativo `GET /api/admin/web-search-settings` lê a configuração e `PATCH /api/admin/web-search-settings` a altera; ambos exigem JWT admin. A migração Go 016 cria essa configuração com DuckDuckGo habilitado para preservar o comportamento de fallback já existente. Após migrar, cada instalação pode desabilitar ou mudar o provedor no painel.

## Pesquisa iterativa (ReAct)

Quando a busca web é acionada e está habilitada, o backend usa `internal/agent` para conduzir um ciclo limitado de pesquisa. O agente pode chamar `search_web` para obter até cinco resultados e `read_page` para extrair conteúdo de uma URL pública. Ele mantém as observações em memória, executa no máximo quatro ações e faz uma síntese final em Markdown com links para as URLs consultadas. O SSE também envia eventos `research` com resumos das consultas, páginas, resultados e síntese; a interface os apresenta num painel recolhível. Esses eventos não incluem o campo `Thought` nem o texto integral das páginas. A resposta final continua chegando pelo SSE e é gravada como mensagem; a trilha de pesquisa é transitória e não é persistida no histórico.

No retorno do agente, o backend extrai apenas o trecho após a última ocorrência de `Final Answer:` e remove campos subsequentes do protocolo e linhas de status da pesquisa. O prompt solicita citações Markdown junto às afirmações factuais; a aplicação valida os URLs citados contra as fontes observadas e corrige a ausência de link inline sem acrescentar uma lista de fontes ao final. O evento SSE `sources` é apresentado como chips clicáveis acima da resposta.

O coletor usa timeout HTTP de 15 segundos, limita o HTML baixado a 8 MiB e o texto extraído a 15.000 caracteres. Remove scripts, estilos, navegação, rodapé, barras laterais e formulários, priorizando `main`, `article` e `role=main`. Respostas 401, 403 e 503 e timeouts viram observações recuperáveis: o agente pesquisa outra fonte, excluindo o domínio bloqueado quando possível. O cancelamento do pedido pelo cliente continua encerrando o trabalho. A validação de rede bloqueia endereços privados/locais e revalida redirecionamentos para reduzir risco de SSRF. Resultados de ferramentas são delimitados como dados externos não confiáveis e nunca devem poder sobrescrever instruções do sistema.

No modo DuckDuckGo, a consulta gerada pelo agente é enviada ao DuckDuckGo. Com SearXNG, a consulta é enviada à instância configurada e aos motores que ela utiliza. A pergunta original pode ser incluída na consulta; não use web search para prompts que contenham dados confidenciais. Se coleta, busca ou inferência iterativa falhar, o chat registra um aviso sem conteúdo sensível e volta à geração normal com o conhecimento geral/RAG, exceto quando o cliente cancela a requisição.

## Instalação

O Compose usa `pgvector/pgvector:pg16`. A migração Go versionada ativa a extensão `vector` e cria a tabela e os índices necessários. Para serviços já existentes no Coolify, execute `cmd/migrate` como etapa de release e confirme que `EMBEDDING_MODEL` está disponível no serviço de inferência:

```bash
# Consulte a documentação do serviço de inferência para disponibilizar o modelo configurado.
```

Adapte utilizador/banco se forem diferentes. A tabela requer vetores com dimensão 768, compatível com o modelo indicado.

## Ingestão síncrona de texto

O endpoint síncrono continua disponível para texto já extraído e aceita no máximo 16 MiB por requisição.

```bash
curl -sS http://localhost:8081/api/documents/ingest \
  -H 'Content-Type: application/json' \
  --data-binary @- <<'JSON'
{
  "title": "Introdução à programação",
  "source_reference": "Universidade Exemplo, Engenharia de Software, 2025, capítulo 2",
  "content": "Cole aqui o texto extraído da apostila. O serviço separará o texto em chunks automaticamente."
}
JSON
```

Resposta esperada:

```json
{"status":"ingested","chunks_count":1}
```

## Ingestão assíncrona de arquivos e web

Para livros e materiais maiores, use `POST /api/admin/documents/upload-batch` com arquivos `.pdf`, `.txt`, `.md`, `.docx` ou `.pptx`; arquivos `.zip` podem conter esses tipos. O limite é 200 MiB por arquivo, 20 arquivos e 200 MiB combinados por lote. PDF com texto selecionável é extraído página a página; digitalizações precisam de OCR prévio. ZIPs são limitados a 500 entradas, 200 MiB por entrada e 512 MiB descompactados. O nome original de cada arquivo interno vira o título indexado.

`POST /api/admin/documents/scrape-url` enfileira uma URL HTTP(S). O extractor remove conteúdo de scripts, estilos e elementos não semânticos. Hosts locais e endereços IP privados são bloqueados.

Ambos retornam `202 Accepted` com IDs de jobs. Consulte `GET /api/admin/jobs` ou conecte-se a `GET /api/admin/jobs/stream`, que envia snapshots SSE a cada segundo. Os embeddings são calculados em lotes de oito chunks e gravados transacionalmente por lote. Para um job de arquivo que falhou, o painel permite reenviar o mesmo arquivo e continuar do progresso confirmado; a API verifica os chunks já presentes antes de gerar embeddings. Uma nova ingestão criada do zero ainda pode adicionar cópias de um material já indexado.

## Pergunta contextualizada

No campo de mensagem, o botão **Pensar** ativa uma resposta mais analítica. Para famílias conhecidas por suportar o recurso (como Qwen 3 e DeepSeek R1), a API também envia `think: true` ao endpoint de chat do protocolo Ollama. Outros modelos, incluindo Qwen 2.5, recebem a instrução de sistema para conferir premissas, considerar alternativas e responder com mais profundidade, sem o parâmetro nativo incompatível. O resultado ainda depende da capacidade do modelo selecionado. A API não exibe nem persiste o canal privado de raciocínio do modelo: somente o conteúdo final da resposta é transmitido.

```bash
curl -N http://localhost:8081/api/chat \
  -H 'Content-Type: application/json' \
  -H 'Accept: text/event-stream' \
  -d '{"prompt":"Qual é a diferença entre pilha e fila segundo a apostila?"}'
```

Quando fontes forem encontradas, o fluxo começa com:

```text
event: meta
data: {"conversation_id":"..."}

event: sources
data: [{"type":"institutional","title":"Introdução à programação","source_reference":"Universidade Exemplo, ..."},{"type":"web","title":"Resultado de pesquisa","url":"https://..."}]
```

Depois seguem eventos `data: {"text":"..."}` e `data: [DONE]`. A API só envia títulos e referências no evento de fontes, não o texto integral dos chunks.

Se nenhum trecho superar o limiar e a pesquisa web estiver desativada, indisponível ou sem resultados, o modelo responde com seu conhecimento geral. A busca tem timeout curto e falhas não bloqueiam a geração. A pergunta atual é enviada ao provedor selecionado somente quando a regra de busca é acionada. A geração ainda depende do serviço de inferência e do modelo configurados. A API atual requer compatibilidade com o protocolo Ollama.

## Segurança e limites

- Conteúdo recuperado é tratado como dado de referência não confiável. O prompt diz ao modelo para não seguir instruções dentro dos documentos. Essa defesa reduz o risco, mas não substitui revisão humana nem garante resistência perfeita a prompt injection.
- A ingestão síncrona, a ingestão em lote e a captura de URL requerem JWT de administrador. A extração de PDF funciona em documentos com camada de texto; digitalizações precisam de OCR prévio.
- PDF, DOCX, PPTX, texto, Markdown, ZIP e HTML são extraídos como texto; não há OCR, remoção automática de dados pessoais ou verificação de direitos autorais.
- A fila de tarefas é local à instância e em memória. Jobs e progresso são persistidos, mas uma interrupção não restaura arquivos temporários nem reprocessa jobs pendentes; configure uma única réplica ou adote uma fila distribuída antes de escalar horizontalmente.
- O limiar de similaridade `0.4`, tamanho dos chunks e modelo são parâmetros iniciais e devem ser avaliados com material e consultas da instituição.
- O frontend atual pode ignorar `event: sources`; o evento SSE está disponível para a interface mostrar referências numa etapa de integração própria.
- Faça backup da tabela `document_chunks` e estabeleça processo para corrigir ou remover material desatualizado.
