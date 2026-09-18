# Contributing to Styr

Styr is early (pre-v0.1.0) — expect the API and UI to move. Issues and PRs are welcome; for
anything non-trivial, open an issue first so the approach can be agreed before you invest time.

## Dev setup

Requirements: Go `1.26`, Node `22+`. `CGO_ENABLED=0` — Styr is pure-Go, no cgo dependencies.

```bash
git clone https://github.com/jonasthim/styr.git
cd styr
go build ./...          # sanity check the backend builds
cd web && npm ci         # install frontend deps
```

### Running the backend against a fake Claude CLI

`make dev-backend` runs `styr serve` with `dev.config.yaml`, which points `claude_bin` at
`testdata/fake-claude/fake-claude.sh` — a shell script that replays a recorded JSONL fixture
(`internal/harness/claude/testdata/*.jsonl`) instead of spawning the real `claude` CLI. This
gives you a working session flow, including the permission round trip, with no Claude account
and no network calls:

```bash
make dev-backend
```

Serves the API and embedded SPA at `http://127.0.0.1:8080` (see `dev.config.yaml`). Never point
`claude_bin` at a real `claude` binary while iterating unless you specifically mean to — real
sessions consume your Claude usage.

### Running the frontend against a mock API

For frontend-only work, run the Vite dev server against an in-browser mock (MSW) instead of a
real backend:

```bash
cd web && npm run dev:mock
```

This needs no Go backend running at all — `VITE_MOCK=1` swaps in `web/src/mocks/handlers.ts`
and a fake `EventSource` for the live feed, seeded with sample users, sessions and approvals.

### Checks

```bash
make check      # go vet + go test -race ./... + a repo-wide grep that forbidden CLI flags
                 # (--dangerously-skip-permissions, bypassPermissions) never appear
cd web && npm run typecheck && npm run lint && npm run build
```

Run `make check` before every commit; CI runs the same plus the frontend checks and a Docker
build validation.

### Tests

- Go: every package has `_test.go` files; run with `go test -race ./...` (or `make test`).
- Frontend unit tests: `npm run test:unit` (Vitest, excludes `e2e/`).
- End-to-end: `npm run test:e2e` (Playwright), with two project pairs:
  - `mock-desktop` / `mock-phone` — run against the MSW-mocked frontend only, no Go backend;
    fast, and what most PRs should pass locally.
  - `real-desktop` / `real-phone` — run against the real Go backend (`make dev-backend`) driving
    the fake Claude CLI; this is the integration check and what CI's release gate runs before
    tagging a release.

  `npx playwright test --project=mock-desktop` (etc.) runs a single project; with no `--project`
  filter both mock and real backends are started and both pairs run.

## Card-based workflow

v0.1 was built as a sequence of self-contained task cards — see
`docs/superpowers/plans/2026-09-18-styr-v0.1.md` for the full plan, its dependency graph and
each card's file list, steps and commit message. If you're picking up follow-on work in the same
style: read a card's "Files" list and steps in order, touch only those files, and use its exact
commit message. Post-v0.1 contributions don't need to follow the card format, but PRs that touch
auth, tokens or permission routing get a closer look — see [SECURITY.md](SECURITY.md) for scope.

## Conventions

- Conventional commit messages (`feat:`, `fix:`, `test:`, `docs:`, `chore:`).
- Go files over ~400 lines are split by responsibility.
- UI copy is English, sentence case, no exclamation marks.
- Never pass `--dangerously-skip-permissions` or `--permission-mode bypassPermissions` to the
  Claude CLI anywhere, including tests — `make check` greps for this and fails the build if it
  finds either string.
- Never log or return a Claude token in full — only its verification status and a 6-character
  suffix label.
