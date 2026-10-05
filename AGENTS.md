# Repository Guidelines

## Project Structure & Module Organization

`cmd/olr` builds the single `olr` binary (CLI, daemon, and DNS relay). Backend modules live under `internal/`; `internal/webui/assets` embeds the built web app and retains a tracked `.gitkeep` so Go builds work without Node. The React/TypeScript UI lives in `web/src`, organized by feature, route, and shared component; its static assets are in `web/src/assets`. Architecture and operator behavior are documented in `design.md` and `docs/`. Packaging lives in `packaging/`, and CI is in `.github/workflows/`.

## Build, Test, and Development Commands

Use Go 1.27 and Node 24. From the repository root:

- `make all`: build the SPA and host binary (`dist/olr`).
- `make build`: build Go without rebuilding the UI.
- `make check`: run Linux-targeted `go vet` and `go test ./...`.
- `make dev`: start a scratch daemon; run `cd web && npm run dev` separately for the UI.
- `make web-deps NPM_INSTALL=ci`: install locked UI dependencies; `cd web && npm run lint` runs oxlint.
- `make package`: build amd64/arm64 `.deb` files.

## Coding Style & Naming Conventions

Use `gofmt` and focused lowercase Go packages. Keep `*_test.go` beside implementation with `TestXxx` functions. UI code uses two-space indentation, single quotes, no semicolons, and PascalCase components; group features under `web/src/features/<feature>`.

## Testing Guidelines

Test behavior changes and update `cmd/olr/testdata` help fixtures when CLI output changes. Run `make check`; for UI changes, run `npm run lint` and `npm run build` in `web/`. CI checks Go and UI separately; no coverage threshold is configured.

## Commit & Pull Request Guidelines

Use short, imperative, sentence-case commit subjects (for example, `Simplify DNS access control ownership`). PRs should explain why and what changed, link issues or design docs, list checks, and include screenshots for UI changes. Flag system or network effects; prefer `--dry-run` on live routers.

## Agent Worktree Workflow

For code changes, create a task branch in a separate worktree (e.g., `git worktree add -b agent/<task> ../olr-<task> main`). Edit, test, and commit there. Fetch and rebase onto the latest `origin/main`, then push the task branch directly to remote `main` (`git push origin HEAD:main`). If `main` advances during the task or the push is rejected because remote `main` moved, fetch and rebase again, resolve conflicts without discarding work, and retry the push; do not stop merely to ask about normal concurrent commits. Never force-push. If a rebase conflict cannot be resolved safely, report the blocker instead of overwriting changes. If local `main` is clean, fast-forward it to the pushed commit; if it has uncommitted changes, leave it untouched. Remove the worktree and task branch after the push.
