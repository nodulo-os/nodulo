# Implantação com Coolify

O Compose da raiz define os serviços da aplicação. No Coolify, associe seu domínio ao serviço web e use o proxy gerenciado pela plataforma para HTTPS. O Compose não inicia um proxy próprio nem publica as portas 80/443.

## Topologia sugerida

1. **PostgreSQL 16/pgvector:** serviço persistente com volume e backup agendado pelo Coolify para armazenamento S3 compatível.
2. **Serviço de inferência:** endpoint compatível com a API Ollama atual, escolhido pela instalação. Pode estar na rede privada ou num serviço externo aprovado pela instituição.
3. **API Go:** recurso Docker apontando para `core-nodulo/Dockerfile`; porta de container `8080`.
4. **Web Next.js:** recurso Docker apontando para `web-nodulo/Dockerfile`; porta de container `3000`.

Todos os serviços que precisam conversar devem compartilhar a rede privada do Coolify. Não use `localhost` para referir-se a outro container.

## Configuração da API

Defina no ambiente do serviço API:

```text
PORT=8080
FRONTEND_ORIGIN=https://SEU_DOMINIO_WEB
INFERENCE_BASE_URL=https://URL-DO-SERVICO-COMPATIVEL
GENERATION_MODEL=IDENTIFICADOR-DO-MODELO-DE-GERACAO
EMBEDDING_MODEL=IDENTIFICADOR-DO-MODELO-DE-EMBEDDINGS
DATABASE_URL=postgres://UTILIZADOR:SENHA@NOME_INTERNO_POSTGRES:5432/BANCO?sslmode=verify-full
JWT_SECRET=SEGREDO_ALEATORIO_FORTE_E_EXCLUSIVO
```

Use o hostname interno fornecido pela rede do Coolify e confirme se o PostgreSQL apresenta um certificado confiável que corresponda a esse hostname. Se o provedor não oferecer TLS no tráfego interno, mantenha os serviços numa rede privada isolada e documente a exceção. Configure uma etapa pré-deploy/one-off que construa `core-nodulo/Dockerfile` com `MAIN_PACKAGE=./cmd/migrate` e execute a imagem resultante apontando para o `DATABASE_URL` de produção. O runner aplica somente versões pendentes e serializa execuções concorrentes. Para criar o administrador inicial, execute `go run ./cmd/seed-admin` em ambiente de release com as variáveis documentadas em [Dados e migrações](06-dados-e-migracoes.md), usando uma senha forte por secret; alternativamente, construa a mesma imagem com `MAIN_PACKAGE=./cmd/seed-admin`.

## Configuração do frontend

Defina `NEXT_PUBLIC_API_URL` como argumento de build Docker com URL que o navegador do utilizador possa alcançar, por exemplo `https://api.exemplo.edu.br/api/chat`. É um valor público embutido durante a compilação; mudar apenas uma variável de runtime não altera o bundle já construído. Reconstrua o frontend ao mudar esse endereço.

Configure domínio e TLS no Coolify para o serviço web e, se separado, para a API. Permita CORS exatamente para a origem do frontend. Se a API estiver atrás de proxy, confirme que ele preserva streaming SSE sem buffering e mantém conexões abertas durante a geração.

Para uma única origem, o proxy pode encaminhar `/api/*` à API e as outras rotas ao Next.js. Configure `NEXT_PUBLIC_API_URL` no build do frontend como `https://SEU_DOMINIO/api/chat`, e `FRONTEND_ORIGIN`/`APP_PUBLIC_URL` como `https://SEU_DOMINIO`. O proxy deve preservar streaming SSE e não pode armazenar respostas autenticadas em cache. O Coolify gerencia o domínio e o certificado HTTPS no proxy; não adicione outro serviço TLS ao Compose.

## Saúde e portas

- API: `GET /api/health` na porta interna `8080`.
- Web: healthcheck da imagem consulta `/` na porta `3000`.
- Não publique PostgreSQL nem o serviço de inferência na internet sem necessidade; prefira rede privada.

## Operação

- Use volumes persistentes para banco e modelo.
- No PostgreSQL do Coolify, abra **Backups → Add**, agende o dump (por exemplo, diariamente às 02:00), configure retenção local e associe um S3 compatível para manter cópia fora do host. Habilite criptografia no destino S3 e limite as credenciais a esse bucket.
- Use **Back up now** e confirme que o job terminou com sucesso, incluiu `nodulo` e enviou o arquivo ao S3. Depois restaure uma cópia numa base descartável pela tela **Import Backup** e confirme que a API consegue ler os dados. O Coolify cria dumps PostgreSQL custom-format e oferece agendamento e upload S3; um backup concluído ainda precisa de restauração verificada. [Backups do Coolify](https://coolify.io/docs/databases/backups) e [restauração](https://coolify.io/docs/databases/restore).
- No Compose standalone, habilite `--profile backup`; o volume local não é uma cópia de recuperação contra perda do host. Copie o dump para destino externo criptografado e ensaie a restauração.
- Fixe versões/imagens após validação; evite depender de `latest` em produção.
- Monitore uso de memória e espaço da infraestrutura de inferência, falhas do provedor, conexões PostgreSQL e latência de geração.
- Defina `JWT_SECRET` com um segredo aleatório guardado no ambiente do Coolify. O fluxo atual oferece JWT, rate limit em login/cadastro e isolamento das conversas; recuperação de conta e gestão de sessões continuam pendentes.
- Documente o processo de atualização e reversão de cada universidade.
