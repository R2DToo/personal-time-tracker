# AGENTS.md

Notes for coding agents (and humans) working on Personal Time Tracker.
User-facing docs are in `README.md`; the original design and plan are in
`docs/superpowers/specs/` and `docs/superpowers/plans/`.

## What this is

A single-user timesheet each consultant runs locally in Docker. They log
decimal hours per project per day on a weekly grid, then read per-project
monthly totals when writing their invoice. There are no rates, notes,
logins, sync, or CSV export, on purpose. Keep it small.

## Hard rules

- **Zero third-party dependencies.** This is the project's main security
  requirement (supply-chain hardening). Go standard library only: `go.mod`
  has no `require` lines and there is no `go.sum`, and the Docker build
  fails if either appears. The frontend is plain HTML/CSS/JS with no npm,
  frameworks, CDNs, or build step. Don't add any of these. If something
  seems to need a library, write the small piece yourself or ask.
- **No network fetches at build time.** Builds and tests run with
  `GOTOOLCHAIN=local GOPROXY=off GOFLAGS=-mod=readonly`.
- **Base images are pinned by `@sha256:` digest** in `Dockerfile` and
  `scripts/test.sh`. They're bumped by hand only (see README).
- **Don't weaken the container or request guard.** The container runs
  non-root, with a read-only rootfs, `cap_drop: ALL`, `no-new-privileges`,
  and the port bound to `127.0.0.1`. See `internal/web/guard.go` for the
  request checks.
- **Never lose a user's hours.** Data writes are atomic, and a corrupt
  `data.json` is refused, never overwritten. Restores take a pre-restore
  snapshot first.

## Testing

Go isn't required on the host. Everything runs in the pinned Go image:

```sh
scripts/test.sh                                   # go vet + go test ./...
scripts/test.sh -run TestMonth ./internal/store/  # a subset
docker compose up -d --build                      # run the real app on :24871
```

Work test-first: write a failing test, watch it fail, then fix. Every bug
fix gets a regression test. There are no automated UI tests in the repo.
UI changes were checked by driving a throwaway headless Chromium over the
DevTools protocol. At minimum, run the app and click through the page you
changed, in both light and dark mode and at phone width (under 640px).

## Layout

| Path | Role |
|------|------|
| `main.go` | Env (`PORT`, default 24871; `DATA_DIR`, default `/data`), the CLI flags `-healthcheck`, `-list-snapshots`, `-recover-snapshot NAME`, an hourly daily-snapshot ticker, and graceful shutdown |
| `internal/store/` | All data logic, with no HTTP |
| `store/hours.go` | `Hours` is integer hundredths. JSON is parsed from the raw token text, so floats are never involved. `ParseDate` is strict and UTC |
| `store/errors.go` | Errors carry sentinel kinds (`ErrInvalid`, `ErrNotFound`, `ErrDuplicateName`) via `errorf`/`asInvalid`, with no prefix on the message |
| `store/model.go` | `Data`/`Project`/`Entry`, `Validate`, name normalisation (`nameKey` is case-insensitive, trimmed, at most 100 runes) |
| `store/store.go` | `Open`, strict decode, atomic `writeFileAtomic`, and `commit` (validates before every write, under a mutex) |
| `store/entries.go` | `SetHours` (0 deletes the entry), `Week` (must start on a Monday), `Month` (counts only that month's days) |
| `store/backup.go`, `store/snapshot.go` | Download/restore, the 30-day backup reminder, daily snapshots (keeps 14), pre-restore snapshots (kept forever), recovery |
| `internal/web/api.go` | `NewHandler` routes (Go 1.22 mux patterns). `writeErr` maps error kinds to status codes: 400, 404, 409, 413, or 500 |
| `internal/web/guard.go` | Wraps every request. Checks the loopback Host allowlist, `Sec-Fetch-Site`, `X-PTT-Request: 1` + JSON + Origin on mutations, and body limits. Sets CSP and cache headers |
| `internal/web/static/` | Embedded frontend. Build DOM only with `el()`/`textContent`, never `innerHTML`. The strict CSP forbids inline script and style |

## Conventions

- Add new user-visible error cases as a kind in the store, and let
  `writeErr` map them to a status. Don't build status codes in handlers.
- API lists must encode as `[]`, never `null`.
- Bump `FormatVersion` in `model.go` only with a migration that keeps
  old backups restorable.
- Commit messages use `feat:`/`fix:`/`docs:` prefixes.

## Known gaps (deliberately deferred)

- Project IDs in restored backup files aren't format-checked, and the
  frontend doesn't `encodeURIComponent` them in URLs.
- Not-found error messages are terse.
- A save failure that happens while navigating away isn't shown.
- `TestAPISnapshotTraversal` accepts 404 as well as 400.
- The backup filename uses the UTC date, not local.

## Decided, don't relitigate without a reason

- Day totals over 24h aren't flagged.
- Comma decimals (`1,5`) are rejected.
- Multiple tabs editing at once is last-write-wins.
- A fresh install gets its first daily snapshot on the next hourly tick.
