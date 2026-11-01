# Visão geral, arquitetura e decisões

## Propósito

O NÓDULO é um projeto em preparação para publicação comunitária, pensado para pessoas e organizações que valorizam autonomia e soberania sobre seus dados. É voltado a universidades e escolas, mas também a empresas e uso individual. O projeto não vende nem monetiza dados de quem o utiliza. Cada instalação escolhe onde hospedar interface, API, banco e motor de inferência, conforme sua infraestrutura e suas políticas. A licença ainda não foi escolhida; até sua publicação, não se deve redistribuir o código como software open source licenciado.

## Componentes

```text
Navegador
   │ HTTP + SSE
   ▼
Next.js (web-nodulo) ── POST /api/chat ──► API Go (core-nodulo)
                                            ├──► Serviço de inferência configurado
                                            └──► PostgreSQL + pgvector
```

- **Frontend:** Next.js 15 App Router, React e TypeScript. Renderiza Markdown, conversa com a API e lê eventos SSE no navegador.
- **API:** Go com biblioteca padrão para HTTP e `pgx/v5`/`pgxpool` para PostgreSQL. Encaminha prompts ao serviço de inferência, persiste mensagens e transmite fragmentos enquanto são gerados.
- **Persistência:** PostgreSQL 16. Conversas pertencem a um utilizador; mensagens pertencem a uma conversa.
- **Inferência:** o serviço compatível com API Ollama configurado por `INFERENCE_BASE_URL` executa o modelo indicado por `GENERATION_MODEL`. O modelo de embeddings é configurado separadamente por `EMBEDDING_MODEL`.
- **Infraestrutura:** Docker Compose conecta web, API e PostgreSQL numa rede privada. O provedor de inferência é configurado por instalação e pode ser local ou remoto.

## Decisões atuais

### Go HTTP e SSE

A API usa `net/http` e mantém o streaming sem timeout global de escrita no servidor. O cliente pode cancelar a geração fechando a requisição; nesse caso a API tenta salvar a resposta parcial com um contexto independente e limitado a cinco segundos.

### Pool PostgreSQL

`pgxpool` é criado e testado com `Ping` na inicialização. O pool tem máximo de 10 conexões, mínimo de 2 e vida máxima de uma hora. O serviço encerra se o banco não estiver acessível.

### Dados locais por padrão

O Compose publica as portas da aplicação somente em `127.0.0.1` por padrão e usa credenciais configuradas no `.env`. O volume nomeado preserva PostgreSQL ao parar os containers. O comando `docker compose down -v` apaga esses dados. O perfil `backup` adiciona dumps PostgreSQL agendados. Em produção no Coolify, o domínio e HTTPS ficam a cargo do proxy gerenciado pela plataforma.

### Compose versus Coolify

O Compose da raiz é o ambiente de desenvolvimento integrado. No Coolify, implante web e API como serviços próprios e ligue-os ao PostgreSQL e ao endpoint de inferência por endereços de rede internos apropriados. Veja [o guia de Coolify](07-implantacao-coolify.md).

## Limites conhecidos e próximos passos

- A API autentica por JWT, limita conversas ao usuário autenticado e aplica rate limits em login e cadastro. Recuperação de conta, gestão de sessões e controles completos de retenção/governança ainda precisam ser planejados antes de uso institucional com dados sensíveis.
- A origem CORS aceita uma única origem configurada em `FRONTEND_ORIGIN`.
- O esquema é controlado por migrações Go versionadas; a API possui autenticação JWT e gestão administrativa de papéis e remoção de contas. Recuperação de conta e gestão de sessões continuam pendentes.
- Backups locais são opcionais no Compose e ficam no mesmo host; em produção, configure cópias externas criptografadas e restaurações testadas no Coolify. Observabilidade, limites de uso, atualização coordenada do banco e recuperação de desastre ainda precisam ser planejados.
- O RAG atual ingere texto, recupera até três chunks acima de um limiar fixo de similaridade e envia referências pelo SSE. Avaliação de qualidade, deduplicação, extração de documentos e gestão de versões ainda precisam evoluir.
- A licença, organização mantenedora e responsáveis por suporte ainda devem ser definidos antes da publicação comunitária. Consulte `SECURITY.md` para o procedimento de relato privado de vulnerabilidades.

Estas limitações são pontos de trabalho explícitos para tornar instalações multiusuário e institucionais seguras. Cada universidade deve revisar requisitos de proteção de dados, acessibilidade, segurança e governança antes do uso real.
