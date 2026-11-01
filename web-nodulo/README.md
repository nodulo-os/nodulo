# Interface web do NÓDULO

Aplicação Next.js com App Router para a landing page, autenticação, chat, perfil e painel administrativo.

## Executar em desenvolvimento

Requisitos: Node.js 20 e npm. Configure `web-nodulo/.env.local`:

```dotenv
NEXT_PUBLIC_API_URL=http://localhost:8081/api/chat
```

Instale dependências e inicie o servidor:

```bash
npm ci
npm run dev
```

A interface fica em <http://localhost:3000>. A API deve estar disponível e configurada com `FRONTEND_ORIGIN=http://localhost:3000`.

## Build de produção

```bash
npm run lint
npm run build
npm run start
```

`NEXT_PUBLIC_API_URL` é incorporada ao bundle durante o build. Configure-a antes de construir a imagem Docker. Consulte o [guia de desenvolvimento](../docs/04-desenvolvimento.md) e o [início rápido](../docs/02-inicio-rapido.md) na raiz.
