# Repository Guidelines

## Project Structure & Module Organization

The Go application lives in `cmd/javinizer/`, with supporting commands in `cmd/coveragecheck/`. Core packages are grouped by responsibility under `internal/` (for example, `api`, `database`, `scraper`, and `worker`). Keep package tests beside implementation files as `*_test.go`; shared fixtures belong in `testdata/` or a package-level `testdata/` directory. The SvelteKit frontend is in `web/frontend/src/`, with Vitest tests near source and Playwright scenarios in `web/frontend/tests/e2e/`. Generated Swagger output is under `docs/swagger/`; configuration examples are in `configs/`.

## Build, Test, and Development Commands

- `make build` builds the frontend and embeds it into `bin/javinizer`.
- `make run` runs the CLI; `make run-api` starts the API server.
- `make web-dev` starts the frontend development server with hot reload.
- `make test-short` runs fast Go tests suitable for pre-commit checks.
- `make test` runs the complete Go suite verbosely; `make web-test` runs Vitest.
- `make ci-full` runs vetting, linting, vulnerability and coverage checks, race tests, config validation, and frontend tests.

Run `make help` for Docker, cross-platform build, Swagger, and other targets. Go 1.26+, CGO (SQLite), and Node.js 20+ are required for full builds.

## Coding Style & Naming Conventions

Format Go code with `make fmt` (`gofmt`) and validate it with `make vet` and `make lint` (`golangci-lint`). Follow standard Go naming: short lowercase package names, exported identifiers in `PascalCase`, and unexported identifiers in `camelCase`. Keep interfaces focused and errors contextual. For Svelte/TypeScript, follow the existing component conventions and run `npm run check --prefix web/frontend` before submitting UI changes.

## Testing Guidelines

Use Go's `testing` package, typically with `testify`; name tests `TestThing_Scenario` and prefer table-driven cases. Mark slow or integration-dependent tests so `-short` can skip them. Frontend unit tests use Vitest, while browser flows use Playwright (`npm run test:e2e --prefix web/frontend`). New behavior should include regression coverage. `make coverage-check` enforces the repository's 75% line-coverage threshold; run `make test-race` for concurrency changes.

## Commit & Pull Request Guidelines

Recent commits use concise, imperative summaries, often in Korean, describing one logical change; merge commits retain the PR number. Keep commits focused and avoid unrelated generated changes. Pull requests should explain the problem and solution, link relevant issues, list verification commands, and include screenshots for visible UI changes. Regenerate and commit Swagger files after API annotation changes (`make swagger`), and keep `configs/config.yaml.example` synchronized with defaults (`make config-drift`).

## Branch & Publishing Workflow

- Base repository work directly on `feature/mediainfo-source-tags` and perform changes on that branch.
- Do not create `agent/*`, fork, worktree, or task branches unless the user explicitly requests one.
- Commit completed changes and push them directly to `origin/feature/mediainfo-source-tags` so the image-build workflow can start from the branch push.
- Do not open a pull request unless the user explicitly requests one. Fork PRs can expose both the upstream repository and the fork and make the release path ambiguous.
