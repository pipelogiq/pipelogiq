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

- Node.js 20+ (or current LTS)
- npm
- Running backend API (`pipeline-api`)

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

## Environment

`VITE_API_BASE_URL` controls API requests.

- If set to absolute URL (example: `http://localhost:8080`), the app calls that host directly.
- If set to relative path (example: `/api`), requests go through Vite (or reverse-proxy) path rewriting.

WebSocket URL is derived automatically:

- Absolute `VITE_API_BASE_URL` -> converted to `ws://.../ws` or `wss://.../ws`
- Relative/unset -> uses current host + `/ws`

Available env files:

- `.env`
- `.env.development`
- `.env.production`

Example:

```env
VITE_API_BASE_URL=/api
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
