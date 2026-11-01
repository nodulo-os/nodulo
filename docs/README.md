# Documentação do NÓDULO

Este conjunto de guias explica o sistema e as decisões atuais para estudantes, docentes, equipes de infraestrutura e pessoas que contribuem com o código. Comece pelo guia rápido se deseja executar o projeto; consulte os demais conforme sua atividade.

## Percursos

- **Quero executar localmente:** [Início rápido](02-inicio-rapido.md) → [Configuração](03-configuracao.md) → [Solução de problemas](08-solucao-de-problemas.md).
- **Quero desenvolver:** [Visão geral e arquitetura](01-visao-geral.md) → [Ambiente de desenvolvimento](04-desenvolvimento.md) → [API](05-api.md) → [Contribuição](09-contribuicao.md).
- **Quero ensinar ou estudar o sistema:** [Visão geral](01-visao-geral.md), [Dados e migrações](06-dados-e-migracoes.md) e [API](05-api.md).
- **Quero ingerir documentos e entender o RAG:** [RAG e conhecimento acadêmico](10-rag.md).
- **Quero configurar pesquisa web privada:** [RAG e pesquisa web configurável](10-rag.md#pesquisa-web-configurável).
- **Quero conectar um motor de IA:** [Configurar o serviço de inferência](11-inferencia.md).
- **Quero executar cálculos e criar gráficos no agente:** [Sandbox Python](13-sandbox-python.md).
- **Quero preparar uma publicação comunitária:** [Publicação no GitHub](12-publicacao-github.md).
- **Quero configurar domínio e backups no Coolify:** [Implantação com Coolify](07-implantacao-coolify.md) e [Backup e restauração](06-dados-e-migracoes.md#backup-e-restauração).
- **Quero implantar numa universidade:** [Configuração](03-configuracao.md) → [Implantação com Coolify](07-implantacao-coolify.md) → [Limites atuais](01-visao-geral.md#limites-conhecidos-e-proximos-passos).

## Guias

1. [Visão geral, arquitetura e decisões](01-visao-geral.md)
2. [Início rápido com Docker Compose](02-inicio-rapido.md)
3. [Configuração e variáveis de ambiente](03-configuracao.md)
4. [Desenvolvimento local](04-desenvolvimento.md)
5. [API HTTP e eventos SSE](05-api.md)
6. [PostgreSQL, esquema e migrações](06-dados-e-migracoes.md)
7. [Implantação com Coolify](07-implantacao-coolify.md)
8. [Solução de problemas](08-solucao-de-problemas.md)
9. [Contribuir e propor mudanças](09-contribuicao.md)
10. [RAG e conhecimento acadêmico](10-rag.md)
11. [Configurar o serviço de inferência](11-inferencia.md)
12. [Preparar publicação no GitHub](12-publicacao-github.md)
13. [Sandbox Python para cálculos e gráficos](13-sandbox-python.md)

Os exemplos usam valores ilustrativos. Configure endpoint e identificadores dos modelos por variáveis de ambiente para cada instalação. Não use credenciais de exemplo em produção.
