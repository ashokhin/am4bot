# am4bot control-plane frontend

React + TypeScript SPA for the multi-tenant control plane (`cmd/apiserver`).
Two locales ship today: English (default/fallback) and Russian — see
`src/i18n/`.

In production this SPA is built and embedded directly into the `apiserver`
binary (`go:embed`, see `internal/webui/embed.go` and
`Dockerfile.controlplane`) — there is no separate frontend container or
`web/Dockerfile` anymore. `npm run dev` below (proxying to a standalone
`apiserver`) remains the normal way to iterate on the UI locally without
rebuilding the Go binary each time.

## Development

```bash
npm install
npm run dev
```

By default the dev server proxies `/api/*` to `http://localhost:8080`
(apiserver's default port). Point it elsewhere with:

```bash
VITE_API_PROXY_TARGET=http://localhost:8090 npm run dev
```

## Build

```bash
npm run build   # type-checks (tsc -b) then builds dist/
npm run lint    # oxlint
```

## End-to-end smoke test

`e2e-smoke.mjs` drives the real dev server with Playwright's bundled
Chromium against a real running `apiserver` — not a mocked one. It exercises
login (including a wrong-password rejection), language switching, session
persistence across a reload, admin user creation, enable/disable, and that a
non-admin is refused the admin user list.

Needs, running beforehand:

- A real Postgres the target apiserver is configured against (an ephemeral
  `postgres:17-alpine` container is enough).
- `apiserver` itself, with an admin user seeded directly in its database
  (there's no signup flow — admins are created out of band).
- The dev server (`npm run dev`), proxying to that apiserver.

Then, with the dev server reachable at `http://localhost:5173`:

```bash
npx playwright install chromium   # once, if not already installed
npm run e2e:smoke
```

It's a standalone script, not part of `npm run build`/CI — there's no
disposable-database plumbing for the frontend the way `internal/store`'s and
`internal/api`'s Go integration tests have (`TEST_DATABASE_URL`, `-tags=
integration`). Re-run it manually after any change that touches login, the
admin users page, or i18n.
