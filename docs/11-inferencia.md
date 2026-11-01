# Configurar o serviço de inferência

O serviço de inferência é escolhido por cada instalação e executa os modelos de geração e embeddings. O NÓDULO não inicia nem administra esse serviço automaticamente pelo Compose principal.

## Configuração

Defina as variáveis no `.env` do backend ou no ambiente de implantação:

```dotenv
INFERENCE_BASE_URL=https://ENDERECO-DO-SERVICO
GENERATION_MODEL=IDENTIFICADOR-DO-MODELO-DE-GERACAO
EMBEDDING_MODEL=IDENTIFICADOR-DO-MODELO-DE-EMBEDDINGS
VISION_MODEL=llama3.2-vision
```

`INFERENCE_BASE_URL` deve ser a URL base sem o caminho `/api`. Em Docker, use um hostname acessível pela rede do container da API; `localhost` aponta para o próprio container. Os identificadores de modelos devem existir no serviço configurado.

Para analisar imagens no chat, instale também um modelo com visão e configure seu identificador em `VISION_MODEL` (por exemplo, `llama3.2-vision` ou `llava`; `LLM_VISION_MODEL` é aceito como alias). O frontend aceita PNG, JPEG e WebP; a API valida a extensão contra a assinatura binária antes de converter a imagem para Base64 e enviá-la ao endpoint `/api/chat`. Mensagens com imagens seguem um fluxo visual direto, sem recuperação pgvector nem pesquisa web; mensagens sem imagens continuam usando `GENERATION_MODEL` com o fluxo RAG/pesquisa configurado. A análise não funciona até que o modelo visual compatível esteja configurado e disponível.

## Compatibilidade técnica atual

Os nomes das variáveis são genéricos, mas a implementação atual fala endpoints e formatos compatíveis com a API Ollama, incluindo `/api/chat`, `/api/embeddings`, `/api/tags`, `/api/pull` e `/api/delete`. Serviços com outro protocolo ainda precisam de um adaptador no backend.

O modelo configurado em `EMBEDDING_MODEL` precisa retornar vetores de 768 dimensões, pois a coluna atual usa `vector(768)`. Se a dimensão não corresponder, a ingestão falha e a busca RAG não funciona.

O modelo de geração e o de embeddings podem ser modelos distintos. Configure somente identificadores que o endpoint consiga servir. A gestão de modelos no painel administrativo também utiliza endpoints Ollama compatíveis no estado atual.

## Considerações de privacidade

Se o endpoint estiver fora da infraestrutura da organização, prompts e trechos recuperados podem ser enviados a esse serviço. Avalie contrato, retenção, telemetria, política de treinamento, localização e controles de segurança do provedor antes de conectar dados institucionais. O NÓDULO não consegue impor políticas internas a um serviço externo.

Prefira uma rede privada ou TLS. Não coloque chaves de acesso em `NEXT_PUBLIC_*` nem em URLs que possam aparecer em logs. O adaptador atual não implementa autenticação arbitrária de provedores; adicione suporte explícito antes de usar um endpoint que exija cabeçalhos próprios.

## Verificação

O painel administrativo mostra se o endpoint responde à consulta de modelos. Para verificar compatibilidade manualmente, adapte o endereço abaixo ao serviço e confirme que ele retorna a lista de modelos:

```bash
curl --fail --silent --show-error "$INFERENCE_BASE_URL/api/tags"
```

Se o endpoint estiver num container, execute a verificação a partir do container da API ou de um container na mesma rede; um teste no host não comprova que a rede da API consegue alcançá-lo.
