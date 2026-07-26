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

Prefer the smallest focused tests that cover the changed behavior. `make test-short` is not a mandatory pre-commit step when scoped package or frontend tests provide sufficient coverage; do not run it unless the breadth or risk of the change makes repository-wide Go validation necessary, or the user explicitly requests it. Frontend-only changes should not trigger unrelated batch or backend test suites.

## Translation Prompt Validation

- Whenever Korean JAV translation prompts or LLM result-processing rules are changed, add or update a focused regression test and run a local LLM test against the affected movie IDs or source text before considering the work complete.
- Inspect the raw LLM response and the final parsed title and description. Reaching the prompt request, receiving HTTP 200, or merely completing the command is not sufficient validation.
- Treat files and databases under `real-data/` as read-only operational data. When real records are required for an LLM test, create a SQLite backup in a temporary directory and run the reprocessing command as a dry-run against that copy; never persist test translations to the operational database.
- Prompt-only changes require focused translation tests and affected-record LLM checks, not the full `make test-short` suite. If the configured local LLM is unavailable, report that live validation remains incomplete instead of claiming the prompt is verified.

## Commit & Pull Request Guidelines

Recent commits use concise, imperative summaries, often in Korean, describing one logical change; merge commits retain the PR number. Keep commits focused and avoid unrelated generated changes. Pull requests should explain the problem and solution, link relevant issues, list verification commands, and include screenshots for visible UI changes. Regenerate and commit Swagger files after API annotation changes (`make swagger`), and keep `configs/config.yaml.example` synchronized with defaults (`make config-drift`).

## Branch & Publishing Workflow

- `main` is reserved exclusively for synchronizing with upstream. It is not the user's working, release, or publishing branch; do not switch to it, base routine changes on it, commit feature work to it, or push ordinary work to it unless the user explicitly requests an upstream-sync operation.
- Base repository work directly on `feature/mediainfo-source-tags` and perform changes on that branch.
- Do not create `agent/*`, fork, worktree, or task branches unless the user explicitly requests one.
- Commit completed changes and push them directly to `origin/feature/mediainfo-source-tags` so the image-build workflow can start from the branch push.
- Do not open a pull request unless the user explicitly requests one. Fork PRs can expose both the upstream repository and the fork and make the release path ambiguous.
