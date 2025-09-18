# Repository Guidelines

## Onboarding & Required Reading
Before coding, You must first read `steering/product.md`, `steering/structure.md`, `steering/tech.md`, `design.md`, `requirements.md`, and `tasks.md`. Mark completed checklist items directly in `tasks.md` and create a Git commit immediately after each task is checked off. Use the MCP tools (`octodoc`, `ref`) to search and get task relavant documentation/references that will help you with the implementation before implementing changes.

## Project Structure & Module Organization
- `cmd/server`: MCP server entrypoint and CLI wiring.
- `internal/runtime`: concurrency limits, semaphores, and guardrails.
- `internal/registry`: tool registration, protocol schemas, middleware.
- `internal/workbooks`: Excelize adapters, workbook cache, validation.
- `pkg`: reusable utilities intended for external sharing.
- `config`: default limits, allow-list templates, environment overrides.
- `testdata`: sanitized `.xlsx` fixtures—never commit sensitive data.

## Build, Test, and Development Commands
- `make run` / `go run ./cmd/server --stdio`: start the server over stdio.
- `make build`: compile the server binary into `cmd/server`.
- `make lint`: run gofmt, goimports (when available), and go vet.
- `make test`: execute unit tests with coverage across `./...`.
- `make test-race`: run race-enabled tests for `internal/...` packages.

## Coding Style & Naming Conventions
Use Go 1.25 tooling: tabs for indentation, gofmt + goimports on save, and idiomatic Go error handling. Follow CamelCase for exported identifiers, snake_case filenames, and `VerbNoun` handler names (e.g., `FilterDataHandler`). Keep packages single-purpose, colocate tests beside implementation, and document new limits in `config/`.

## Testing Guidelines
Author table-driven tests in `*_test.go` files and cover success, validation errors, and busy-limit paths. Use fixtures under `testdata/` for streaming scenarios and guard memory via iterator patterns. Run `make test` for coverage and `make test-race` when touching concurrency or workbook locking logic. Ensure responses assert metadata fields like `total`, `returned`, `truncated`, and `nextCursor`.

## Commit & Pull Request Guidelines
Write imperative commit subjects with clear scope (e.g., `runtime: enforce workbook cap`). Summaries should mention requirements addressed and validation performed (`make lint`, `make test`, race runs if relevant). Pull requests must include description, linked issues/requirements, test evidence, and screenshots or logs for protocol or response changes. Flag configuration updates and request reviewers responsible for touched modules.

## Per-Task GitHub Workflow (Agent Policy)

For every task in `tasks.md`, follow this exact workflow:

1. Create a branch from `main` using a descriptive prefix: `feat/`, `fix/`, `chore/`, `docs/`, or `refactor/`.
2. Implement changes and update related docs (`steering/*`, `design.md`, `requirements.md`, `tasks.md`).
3. Validate locally with `make lint && make test && make test-race`.
4. Commit and push; open a PR to `main` via `gh pr create -B main -H <branch>`.
5. Wait for green CI (`.github/workflows/ci.yml`); address any findings.
6. Merge via `gh pr merge --squash --delete-branch` to keep history clean.
7. Refresh local `main` with `git checkout main && git pull`.
8. If appropriate, tag and publish a release: `git tag vX.Y.Z -m "..." && git push origin vX.Y.Z && gh release create vX.Y.Z --generate-notes`.

Do not push directly to `main`. Ensure all configuration and documentation changes accompany code changes in the same PR when relevant.

## Agent Note: Versioning Policy
- After completing each task in `tasks.md`, bump the patch version and publish a release.
- API model: path-only. Tools accept a canonical `path` (or `cursor`). Do not introduce `workbook_id` flows.
- When all tasks currently listed in `tasks.md` are complete, bump the minor version.
- Reserve additional patch versions for hotfixes unrelated to task completion.

## Security & Configuration Tips
Respect directory allow-lists when accessing workbooks; never bypass via manual path joins. Keep operations bounded (≤10k cells, ≤128KB payload) and surface new limits through metadata. Document environment or config additions in `config/` and `design.md`. Use existing logging and middleware hooks instead of ad-hoc prints for telemetry or audits.
