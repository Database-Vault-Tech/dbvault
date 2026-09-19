# DBVault frontend

Next.js 16 (App Router) dashboard and landing page for DBVault, built with Tailwind CSS v4,
shadcn/ui (Radix), TanStack Query, React Hook Form and Zod.

```bash
npm install
API_URL=http://localhost:8080 npm run dev   # http://localhost:3000
npm run lint && npm run typecheck && npm test
npm run test:e2e                            # needs a running stack; see docs/development.md
```

- `src/app/page.tsx` → `/` landing page (`src/components/marketing`)
- `src/app/(auth)` → login, registration, password reset, invitations
- `src/app/(app)` → the dashboard (auth-gated by `src/proxy.ts` and the API)
- `src/app/api/[...path]/route.ts` streams `/api/*` to the Go API at `API_URL` (read at runtime)
- `src/lib/api.ts` (fetch client with CSRF + org headers), `src/lib/queries.ts` (TanStack Query hooks), `src/lib/types.ts` (API types)

See the repository [README](../README.md) and [docs/development.md](../docs/development.md).
