# Executor Python isolado no Coolify

O backend do NÓDULO envia scripts ao broker privado `python-executor`. Apenas esse broker acessa o socket Docker e cria containers efêmeros com rede desativada, sistema de arquivos somente leitura, usuário sem privilégios, limites de memória/CPU/processos e timeout. A API não recebe o socket nem o Docker CLI.

## Antes de ativar

O broker precisa controlar o daemon Docker para iniciar os containers de execução. O acesso ao socket Docker equivale a controle administrativo sobre o host. Portanto, neste desenho o broker é uma fronteira de confiança: mantenha-o atualizado, sem domínio público, com credencial forte e acessível apenas pela rede privada. Para uma instalação com maior exigência de isolamento, execute o broker em um servidor dedicado com daemon próprio (idealmente rootless ou runtime de sandbox) e conecte-o à API por VPN e HTTPS/mTLS.

## Criar o serviço no Coolify

1. No mesmo servidor Coolify da API, crie um recurso **Docker Compose** apontando para este repositório e para `deploy/python-executor.compose.yml`.
2. Configure as variáveis do recurso:

   ```env
   PYTHON_EXECUTOR_TOKEN=<segredo aleatório com pelo menos 32 caracteres>
   DOCKER_GID=<GID do grupo do socket Docker no servidor>
   PYTHON_EXECUTOR_CONCURRENCY=2
   PYTHON_EXECUTOR_NETWORK=coolify
   ```

   Gere o segredo com `openssl rand -hex 32`. Consulte o GID por SSH no servidor Coolify com `stat -c '%g' /var/run/docker.sock`.

3. Mantenha o executor sem domínio e sem `ports:` publicados. O Compose declara o alias privado `nodulo-python-executor` na rede `coolify`.
4. No recurso da API, defina:

   ```env
   PYTHON_EXECUTOR_URL=http://nodulo-python-executor:8080
   PYTHON_EXECUTOR_TOKEN=<o mesmo segredo do executor>
   ```

   Se a API também for um recurso Compose, habilite **Connect To Predefined Network** para a rede `coolify`. Recursos em servidores diferentes não compartilham essa rede; nesse caso, use VPN/rede privada e HTTPS com autenticação forte.

5. No painel do Coolify, exclua o componente de execução única `sandbox-image-build` da avaliação agregada de saúde. Implante primeiro o executor. Aguarde o healthcheck ficar saudável; ele verifica se o daemon está acessível e se a imagem do sandbox existe. Depois implante ou reinicie a API.
6. Teste no chat uma solicitação de cálculo ou gráfico e confira os logs do broker. O broker registra duração e resultado, sem guardar o código enviado.

## Execução local pelo Compose principal

No `.env` da raiz, defina `PYTHON_EXECUTOR_URL=http://python-executor:8080`, gere `PYTHON_EXECUTOR_TOKEN` e informe `DOCKER_GID`. Depois execute:

```bash
docker compose --profile python up -d --build
```

O perfil `python` constrói a imagem Python e inicia o broker. Sem esse perfil e sem `PYTHON_EXECUTOR_URL`, a ferramenta de execução permanece indisponível e o chat não tenta usar o daemon Docker local da API.

## Imagens do sandbox

`Dockerfile.python-sandbox` fixa Python 3.13, NumPy e Matplotlib. O serviço `sandbox-image-build` constrói a imagem `nodulo-python-sandbox:1.0` no mesmo daemon que o broker usará. Ao atualizar dependências, revise os pins e reimplante o serviço antes de reimplantar o executor.

Python 3.13 recebe correções de segurança até outubro de 2029, conforme o calendário oficial. [Status oficial das versões Python](https://devguide.python.org/versions/). Os pins usados são [NumPy 2.5.4](https://pypi.org/project/numpy/2.5.4/) e [Matplotlib 3.11.2](https://pypi.org/project/matplotlib/3.11.2/).
