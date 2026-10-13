# Repository Guidelines

## Project Structure & Module Organization

Sub2API is split into a Go backend and Vue 3 frontend:

- `backend/cmd/server/` contains the server entry point and version file.
- `backend/internal/` contains configuration, handlers, services, repositories, gateway adapters, and tests.
- `backend/ent/` contains generated Ent ORM code; schemas live in `backend/ent/schema/`.
- `backend/migrations/` contains ordered SQL migrations; `backend/resources/` holds runtime data such as model pricing.
- `frontend/src/` contains Vue components, views, API clients, stores, composables, and utilities; `frontend/public/` contains static assets.
- `deploy/` contains Docker Compose and deployment configuration; `docs/` contains architecture notes.

## Build, Test, and Development Commands

Use Go 1.26.1, Node.js 20, and pnpm 9.

- `pnpm --dir frontend install --frozen-lockfile` installs the locked frontend dependencies.
- `make build` builds the frontend first, then embeds it into the backend binary.
- `make test` runs backend tests/lint plus frontend lint and type checks.
- `make -C backend test-unit` and `make -C backend test-integration` run tagged Go suites.
- `pnpm --dir frontend run test:run` runs Vitest; `pnpm --dir frontend run test:coverage` checks the 80% global thresholds.
- `pnpm --dir frontend run dev` starts the Vite development server.
- `make secret-scan` checks for accidentally committed secrets.

## Coding Style & Naming Conventions

Run `gofmt` (or `golangci-lint run ./...`) for Go. Keep handlers, services, and repositories in their existing layers; the linter rejects forbidden service/handler dependencies. Use Go initialisms such as `API`, `HTTP`, and `ID`. Frontend code follows the repository ESLint configuration: two-space indentation, TypeScript-friendly Vue 3 style, PascalCase component files, and camelCase variables/functions. Use pnpm and update `frontend/pnpm-lock.yaml` with dependency changes. Regenerate Ent/Wire output with `make -C backend generate` when their inputs change.

## Testing Guidelines

Name Go tests `*_test.go` and use `unit` or `integration` build tags where appropriate. Name frontend tests `*.spec.ts` or `*.test.ts` under `src/**/__tests__/`; Vitest runs them in jsdom. Add focused tests for behavior changes and run the relevant tagged suite plus frontend type/lint checks.

## Commit & Pull Request Guidelines

Recent commits use Conventional Commit prefixes such as `feat:`, `fix(scope):`, `test:`, `chore:`, and `merge:`; write an imperative, specific subject. Pull requests should explain the user-visible or operational impact, identify migrations/configuration changes, link related issues, and include screenshots or request/response examples for UI/API changes. Report the validation commands you ran and keep generated files and lockfiles synchronized.

