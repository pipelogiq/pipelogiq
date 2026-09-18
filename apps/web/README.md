# Pipelogiq Web

Frontend dashboard for Pipelogiq, built with React + TypeScript + Vite.

## Stack

- React 19
- TypeScript
- Vite
- React Router
- TanStack Query
- Tailwind CSS + Radix UI components

## Prerequisites

- Node.js 24 LTS (matches Docker and CI)
- npm
- Running backend API (`pipelogiq-api`)

## Install

```bash
cd apps/web
npm install
```

## Run in Development

```bash
npm run dev
```

- Dev server: `http://localhost:3300`
- Vite proxy:
  - `/api` -> `http://localhost:8080`
  - `/ws` -> `ws://localhost:8080`

If another service uses port 8080, set `PIPELOGIQ_DEV_API_TARGET` to the running
Pipelogiq backend. Browser HTTP requests use the same-origin `/api` path and
Vite forwards them to this target. For a Go internal API listening on port 8088:

```bash
PIPELOGIQ_DEV_API_TARGET=http://127.0.0.1:8088 npm run dev
```

For the combined Pipelogiq container exposed on port 3300, include its `/api`
prefix and start Vite on a different port:

```bash
PIPELOGIQ_DEV_API_TARGET=http://127.0.0.1:3300/api npm run dev -- --port 3301
```

In the first example, `/api/users` forwards to `http://127.0.0.1:8088/users`.
In the second, it forwards to `http://127.0.0.1:3300/api/users`. When
`VITE_API_BASE_URL` is unset or relative, WebSocket requests also use Vite's
proxy, which forwards `/ws` to the target origin without an `/api` prefix.
An HTTPS proxy target uses WSS for WebSockets.

You can also save `PIPELOGIQ_DEV_API_TARGET` in `.env.development.local`; restart
Vite after changing it. This setting configures the development proxy only and
is not exposed to browser code. Supported targets use HTTP or HTTPS with an
empty/root path or `/api` (an optional trailing slash is accepted), and cannot
contain credentials, query parameters or a fragment. The default remains
`http://localhost:8080`.

## Environment

HTTP API requests always use the current origin's `/api` path. In development,
`PIPELOGIQ_DEV_API_TARGET` chooses the backend reached through Vite's proxy.
Production uses the deployed reverse proxy.

`VITE_API_BASE_URL` only overrides the pipeline WebSocket destination:

- Absolute `VITE_API_BASE_URL` -> converted to `ws://.../ws` or `wss://.../ws`
- Relative/unset -> uses current host + `/ws`

`VITE_APP_ENVIRONMENT` optionally overrides the environment label shown in the sidebar footer.

Available env files:

- `.env`
- `.env.development`
- `.env.production`

Example:

```env
VITE_API_BASE_URL=/api
VITE_APP_ENVIRONMENT=Production
```

## Scripts

```bash
npm run dev      # start Vite dev server
npm run build    # type-check + production build
npm run lint     # ESLint
npm run preview  # preview built app
```

## Project Layout

```text
apps/web/
  src/
    api/          # API client layer
    components/   # UI and feature components
    contexts/     # React contexts (auth, etc.)
    hooks/        # custom hooks (including WebSocket updates)
    pages/        # route pages
    types/        # TypeScript API/domain types
```

## Auth and Backend Notes

- App auth uses cookie-based login (`/auth/login`) against the internal API.
- API calls include `credentials: "include"`, so backend CORS/cookie settings must allow your frontend origin in non-proxied setups.

## Build Output

- Production assets are emitted to `apps/web/dist`.
