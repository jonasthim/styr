## Summary

<!-- What does this change, and why? -->

## Test plan

<!-- How did you verify this? Commands run, tests added. -->

- [ ] `make check` passes
- [ ] `cd web && npm run typecheck && npm run lint && npm run build` passes (if `web/` changed)
- [ ] Relevant `go test -race ./...` / `npm run test:unit` / `npm run test:e2e` coverage added or
      updated

## Checklist

- [ ] No `--dangerously-skip-permissions` or `--permission-mode bypassPermissions` anywhere
      (`make check` greps for this, but double-check test helpers too)
- [ ] No Claude token, or other secret, logged or returned in full
- [ ] Conventional commit message(s) (`feat:`, `fix:`, `test:`, `docs:`, `chore:`)
