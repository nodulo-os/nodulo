# Sandbox Python para cálculos e gráficos

O Agente ReAct oferece `execute_python` para cálculos matemáticos, estatística e gráficos. A ferramenta executa o código gerado em um container efêmero; não instala pacotes durante uma conversa. O gráfico opcional é salvo em `/tmp/plot.png`, validado como PNG pelo backend e enviado ao navegador como um evento SSE `attachment`.

## Preparar a imagem

Na máquina/daemon Docker que executará os containers do sandbox, construa a imagem versionada incluída no repositório:

```bash
docker build -f core-nodulo/Dockerfile.python-sandbox -t nodulo-python-sandbox:latest core-nodulo
```

A imagem contém Python 3.10, NumPy e Matplotlib. A instalação de dependências acontece apenas durante a construção da imagem.

Configure `NODULO_PYTHON_SANDBOX_IMAGE` se usar outro nome de imagem. A API exige a CLI `docker` no `PATH` e permissão para solicitar containers ao daemon.

## Isolamento aplicado

Cada execução usa `--rm`, `--network=none`, limite de 256 MiB de memória, 0,5 CPU, limite de processos, filesystem raiz somente de leitura, usuário sem privilégios, remoção de capabilities, `no-new-privileges`, `/tmp` temporário de 64 MiB e timeout de 30 segundos. O script e a imagem não recebem volumes do host nem variáveis de ambiente da API. O tamanho do código, stdout/stderr e PNG também é limitado.

O sandbox não é uma fronteira de segurança absoluta contra vulnerabilidades do kernel, do runtime ou do daemon. **Não monte o socket de um Docker rootful (`/var/run/docker.sock`) numa API exposta à rede**: quem controla esse socket pode, na prática, controlar o host. Para produção, execute a API com um broker dedicado de execução ou um daemon rootless isolado numa VM/host descartável, com dados e credenciais separados. O Compose principal não concede acesso ao socket do Docker; sem um daemon de sandbox configurado, `execute_python` retorna indisponibilidade em vez de executar código fora do container.

## Fluxo de resposta

O resultado de console volta ao agente como `Observation`. Se houver PNG válido, a API emite `event: attachment` com `data_url` no formato `data:image/png;base64,...` e grava o gráfico nos anexos JSONB da mensagem do assistente. Ao reabrir o histórico, a interface recupera e exibe a imagem persistida no PostgreSQL. O armazenamento de gráficos gerados tem limite agregado de 8 MiB por resposta.
