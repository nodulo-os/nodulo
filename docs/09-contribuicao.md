# Contribuir e propor mudanças

O NÓDULO é um projeto em evolução para uso educacional e institucional. Relatos claros ajudam estudantes, docentes e mantenedores a reproduzir problemas em ambientes diferentes.

## Antes de enviar uma mudança

1. Leia [arquitetura e limites](01-visao-geral.md) e a documentação da área alterada.
2. Descreva o problema e o contexto (sistema operacional, versões relevantes, configuração sem segredos).
3. Prefira alterações pequenas, com comportamento e impacto documentados.
4. Para mudanças de esquema no banco, acrescente uma migração Go incremental em `core-nodulo/pkg/db/migrations.go`, com versão nova, plano de aplicação e efeito em dados existentes. Não altere versões já publicadas.
5. Para novas variáveis, documente finalidade, exemplo não secreto, momento de leitura (build/runtime) e efeito em implantação.
6. Não inclua `.env`, credenciais, prompts de usuários reais, dumps nem dados acadêmicos identificáveis.
7. Execute as verificações aplicáveis em [Desenvolvimento](04-desenvolvimento.md) e descreva o que executou.

## Decisões com impacto institucional

Mudanças em autenticação, retenção, telemetria, uso de modelos externos, licença, acessibilidade ou compartilhamento de dados precisam explicitar impactos para as instituições e seus usuários. Não suponha que uma decisão adequada a uma universidade se aplique às demais.

## Estado de governança

O repositório ainda precisa que os mantenedores definam licença, organização proprietária, responsáveis por revisão e canal de suporte antes de uma publicação comunitária. O processo de relato privado está descrito em [`SECURITY.md`](../SECURITY.md); antes de publicar no GitHub, habilite Private vulnerability reporting e confirme quem responderá aos relatos. Contribuições e implantação devem considerar esses itens pendentes.
