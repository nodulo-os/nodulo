# Preparar a publicação no GitHub

Este guia organiza a publicação do código e a configuração inicial da comunidade. Antes de tornar o projeto público, os mantenedores precisam decidir o proprietário/organização GitHub e a licença. O README não declara uma licença até que exista um arquivo `LICENSE` aprovado.

## 1. Decisões antes de criar o repositório

- Escolha a conta ou organização que manterá o projeto e o nome final do repositório.
- Escolha e aprove uma licença de software; adicione o texto oficial correspondente em `LICENSE`.
- Indique os responsáveis iniciais por manutenção, revisão e triagem de segurança, e um canal de suporte que será acompanhado.
- Após criar o repositório, habilite **Private vulnerability reporting** em Settings → Security e confirme a opção **Report a vulnerability** na aba Security. O procedimento está preparado em [`SECURITY.md`](../SECURITY.md), mas não pode ser ativado até existir o repositório GitHub.
- Decida quem controla o domínio/site e o nome público do projeto antes de criar badges, e-mails ou canais oficiais.
- Confirme que `.env`, `.env.local`, credenciais, dumps e documentos privados não estão no histórico Git.
- Defina quem revisa pull requests e responde a relatos de segurança e suporte.

## 2. Criar o repositório

No GitHub, crie um repositório vazio, sem README ou `.gitignore` gerados pelo site, porque esses arquivos já estão no projeto. Não escolha uma licença na interface antes da decisão dos mantenedores; hoje ainda não há arquivo `LICENSE`. Para uma publicação comunitária, habilite Issues e Pull Requests; habilite Discussions se houver pessoas responsáveis por moderá-las.

Em uma cópia local que ainda não tenha histórico Git, use o endereço fornecido pelo GitHub:

```bash
git init
git add .
git status --short
git commit -m "docs: prepare NÓDULO project for publication"
git branch -M main
git remote add origin git@github.com:ORGANIZACAO/REPOSITORIO.git
git push -u origin main
```

Revise `git status --short` antes do commit. Os arquivos `.env` e `.env.local` devem permanecer ignorados; os arquivos `.env.example` são modelos sem credenciais.

## 3. Configuração recomendada do repositório

- Proteja `main`: exija pull request e revisão antes de integrar mudanças.
- Mantenha credenciais de deploy nos secrets do ambiente de CI/CD ou do Coolify, nunca em arquivos versionados.
- Configure a política de relato de vulnerabilidades e um canal privado de contato antes de anunciar suporte de segurança.
- Use issues para bugs e propostas; registre decisões arquiteturais que afetem privacidade ou operação institucional.
- Publique instruções claras de suporte e o escopo mantido pela equipe.

Não publique com o badge “Open Source” nem prometa suporte de segurança até que `LICENSE`, mantenedores e relato privado estejam efetivamente configurados. A lista dos itens técnicos já preparados e dos bloqueios de governança deve ser revisada no mesmo PR de publicação.

## 4. Após a publicação

Substitua badges e links provisórios por URLs reais do repositório, confira se os links relativos do README funcionam na página inicial e faça uma instalação limpa seguindo [Início rápido](02-inicio-rapido.md). Mantenha a licença, o processo de contribuição e a documentação de segurança alinhados com as decisões dos mantenedores.
