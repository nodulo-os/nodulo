# Provisionamento do Motor de Inferência (Nuvem)

Este script automatiza a criação do nó de processamento matemático (GPU A100/L4) no Google Colab. 
Ele garante que a infraestrutura pesada seja instanciada sob demanda, atuando como um processador descartável e mantendo a soberania dos dados no servidor local.

## Script de Inicialização (Jupyter Notebook / Google Colab)

```python
!apt-get update -qq
!apt-get install -y zstd curl wget

# 1. Instala o Ollama
!curl -fsSL https://ollama.com/install.sh | sh

# 2. Inicia o servidor Ollama liberando conexões externas (Resolve o 403)
import subprocess
import time
import os

env = os.environ.copy()
env["OLLAMA_HOST"] = "0.0.0.0"   # Escuta em todas as interfaces
env["OLLAMA_ORIGINS"] = "*"      # Aceita conexões de qualquer URL (Cloudflare)

subprocess.Popen(["ollama", "serve"], env=env)
time.sleep(5)

# 3. Baixa o modelo mestre de código (32B) e o de embeddings para o RAG
!ollama pull qwen2.5-coder:32b
!ollama pull nomic-embed-text
!ollama pull llava

# 4. Baixa e inicia o Cloudflare Tunnel
!wget -q https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64
!chmod +x cloudflared-linux-amd64
!./cloudflared-linux-amd64 tunnel --url http://localhost:11434

```