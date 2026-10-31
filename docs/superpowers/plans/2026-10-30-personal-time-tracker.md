# Personal Time Tracker Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A single-user, localhost-only weekly timesheet web app with zero third-party dependencies, shipped as a locally-built Docker container with a persistent volume.

**Architecture:** One Go binary (standard library only). `internal/store` owns the data (a JSON file written atomically, daily snapshots, backup/restore); `internal/web` owns the HTTP layer (request guard, JSON API, embedded static UI). The UI is plain HTML/CSS/vanilla JS with hash routing and a left sidebar.

**Tech Stack:** Go 1.27 stdlib (`net/http` 1.22+ routing patterns, `encoding/json`, `embed`, `log/slog`, `crypto/rand`), vanilla JS, Docker multi-stage build onto distroless static, Docker Compose.

**Spec:** `docs/superpowers/specs/2026-10-30-personal-time-tracker-design.md`

## Global Constraints

- App name, everywhere user-visible: **Personal Time Tracker**.
- Go standard library only: `go.mod` has **no `require` lines** and the repo has **no `go.sum`**. Builds/tests run with `GOTOOLCHAIN=local GOPROXY=off GOFLAGS=-mod=readonly` so any download attempt fails.
- No npm, no bundler, no CDN/external assets. UI files are embedded with `embed`.
- Go module path: `personaltimetracker`. `go 1.27` directive.
- Build image: `golang:1.27-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb`.
- Runtime image: `gcr.io/distroless/static-debian13:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99`. Runs as UID/GID `65532`.
- Default port **24871**, set by one `PORT` value in `.env`; published as `127.0.0.1:${PORT}:${PORT}`.
- Data dir `DATA_DIR` (default `/data`): `data.json` plus `snapshots/`.
- Hours: decimal, 0–24 per cell, max two decimal places; stored internally as integer hundredths (`store.Hours`). Weeks run Monday–Sunday. Dates are `YYYY-MM-DD` strings with no time zone.
- Snapshots: keep 14 newest daily; keep all pre-restore. Backup reminder after 30 days (or never) when any project exists.
- Project names: trimmed, non-empty, ≤ 100 characters *(plan's call)*, unique ignoring case. No project deletion.
- No local Go on the dev host: run tests with `scripts/test.sh` (Task 1), which uses the pinned build image.

## Review Focus

1. **Float noise in sums.** Typing `0.1`, `0.2`, `0.29` must total exactly (`0.29 × 3 = 0.87`, never `0.8699999`). Pinned by `TestHoursRoundTrip` / `TestMonthTotalsExact` (Tasks 1, 3).
2. **Corrupt or hand-edited `data.json` at startup.** The app must refuse to start with a clear error and must not overwrite the file. Pinned by `TestOpenCorruptFileFailsWithoutTouchingIt` (Task 2).
3. **Bad restore file** (unknown version, entry for a missing project, duplicate project/date, hours > 24, duplicate names, unknown fields, not JSON). Rejected with data unchanged. Pinned by `TestRestoreRejectsBadFiles` (Task 4).
4. **Snapshot name path traversal** (`../data.json`, `daily-x.json`, absolute paths) on the restore-snapshot endpoint. Must return 400 and touch nothing. Pinned by `TestRestoreSnapshotRejectsBadNames` (Task 5) and `TestAPISnapshotTraversal` (Task 7).
5. **Week crossing a year/month boundary** (Mon 2026-12-28 to Sun 2027-01-03). Each month counts only its own days. Pinned by `TestMonthSplitsYearBoundaryWeek` (Task 3).

Also guarded, outside the top five: re-cloning into a different folder name must reuse the same volume (compose `name:` pinned, verified in Task 10).

---

## File Structure

```
go.mod                          module personaltimetracker, go 1.27, no requires
main.go                         env/flags, startup, hourly snapshot ticker, -healthcheck, graceful shutdown
scripts/test.sh                 go vet + go test inside the pinned golang image
internal/store/hours.go         Hours type (hundredths), ParseHours, JSON, ParseDate
internal/store/model.go         Data/Project/Entry, errors, Validate
internal/store/store.go         Store: Open, atomic save, projects ops
internal/store/entries.go       SetHours, Week, Month
internal/store/backup.go        DecodeBackup, ExportBackup, Restore, BackupStatus
internal/store/snapshot.go      EnsureDailySnapshot, Snapshots, RestoreSnapshot
internal/store/*_test.go
internal/web/guard.go           Host/CSRF/body-size/security-header middleware
internal/web/api.go             JSON handlers + NewHandler
internal/web/static/index.html  sidebar shell + page containers
internal/web/static/app.css
internal/web/static/app.js      router, pages, API client
internal/web/*_test.go
Dockerfile  docker-compose.yml  .env  .dockerignore  README.md
```

---

### Task 1: Project scaffold, hours and date primitives

**Files:**
- Create: `go.mod`, `scripts/test.sh`, `internal/store/hours.go`
- Test: `internal/store/hours_test.go`

**Interfaces:**
- Produces:
  - `type Hours int64` (hundredths of an hour); `const MaxHours Hours = 2400`
  - `func ParseHours(s string) (Hours, error)`: accepts `^\d+(\.\d{1,2})?$` (also `.5`), value ≤ 24; rejects negatives, exponents, >2 decimals, empty. Error wraps `ErrInvalid`.
  - `func (h Hours) String() string`: minimal decimal: `150→"1.5"`, `400→"4"`, `25→"0.25"`, `0→"0"`.
  - `MarshalJSON` emits `String()` as a bare JSON number; `UnmarshalJSON` parses the raw token text with `ParseHours` (never via float64), and rejects strings/null.
  - `func ParseDate(s string) (time.Time, error)`: strict `2006-01-02`, returns UTC midnight, rejects `2026-02-30` and `2026-1-5`. Error wraps `ErrInvalid`.
  - `var ErrInvalid = errors.New("invalid")` (declare in `hours.go`; Task 2 adds the others alongside it).

- [ ] **Step 1: Scaffold.** `go.mod` with `module personaltimetracker` and `go 1.27` only. `scripts/test.sh` (POSIX sh, executable):

```sh
#!/bin/sh
# Runs vet + tests in the same pinned Go image the Dockerfile uses; no local Go needed.
set -eu
IMAGE="golang:1.27-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb"
[ $# -eq 0 ] && set -- ./...
exec docker run --rm -v "$(pwd)":/src:ro -w /src \
  -e CGO_ENABLED=0 -e GOTOOLCHAIN=local -e GOPROXY=off -e GOFLAGS=-mod=readonly \
  "$IMAGE" sh -c 'go vet ./... && go test "$@"' sh "$@"
```

- [ ] **Step 2: Write failing tests** in `hours_test.go`:
  - `TestParseHours`: table. Valid `"4"→400`, `"1.5"→150`, `"0.25"→25`, `".5"→50`, `"24"→2400`, `"0"→0`. Invalid: `""`, `"-1"`, `"24.01"`, `"1.234"`, `"1e2"`, `"abc"`, `"1.5.5"`, `" 1"`.
  - `TestHoursString`: `150→"1.5"`, `400→"4"`, `25→"0.25"`, `1010→"10.1"`, `0→"0"`.
  - `TestHoursRoundTrip`: `json.Unmarshal([]byte("0.29"), &h)` gives `29`; `Hours(29+29+29).String() == "0.87"`; `json.Marshal(Hours(87))` gives `0.87`; unmarshal of `"\"1\""` and `null` errors.
  - `TestParseDate`: `"2026-10-26"` ok and `.Weekday()==time.Monday`; `"2026-02-30"`, `"2026-1-05"`, `"26-10-26"` error; `errors.Is(err, ErrInvalid)`.

- [ ] **Step 3: Run** `scripts/test.sh ./internal/store/`. Expected: FAIL, `undefined: ParseHours`.
- [ ] **Step 4: Implement** `hours.go`. `ParseDate`: `time.Parse` then require `t.Format(layout) == s`.
- [ ] **Step 5: Run** `scripts/test.sh`. Expected: PASS (vet clean).
- [ ] **Step 6: Commit** `feat: scaffold module and hours/date primitives`.

---

### Task 2: Data model, validation, store open/save, project operations

**Files:**
- Create: `internal/store/model.go`, `internal/store/store.go`
- Test: `internal/store/model_test.go`, `internal/store/store_test.go`

**Interfaces:**
- Consumes: `Hours`, `ParseDate`, `ErrInvalid` (Task 1).
- Produces:
  - `const FormatVersion = 1`
  - `type Project struct { ID string "id"; Name string "name"; Archived bool "archived"; CreatedAt time.Time "createdAt" }` (JSON tags as shown)
  - `type Entry struct { ProjectID string "projectId"; Date string "date"; Hours Hours "hours" }`
  - `type Data struct { Version int "version"; Projects []Project "projects"; Entries []Entry "entries"; LastBackupDownloadAt *time.Time "lastBackupDownloadAt" }`
  - `var ErrNotFound, ErrDuplicateName` (errors wrap these with `fmt.Errorf("%w: …")`)
  - `func Validate(d Data) error`: version == 1; IDs non-empty and unique; names pass `normalizeName` and are unique ignoring case (`strings.EqualFold`); every entry's project exists; date passes `ParseDate`; hours in `1..MaxHours` (0 is never stored); `(projectId,date)` unique. Returns the first problem wrapped in `ErrInvalid` (or `ErrDuplicateName` for names) with a human-readable message.
  - `type Store struct` (unexported fields: `mu sync.Mutex`, `dir string`, `now func() time.Time`, `data Data`)
  - `func Open(dir string, now func() time.Time) (*Store, error)`: creates `dir/snapshots`; if `dir/data.json` is missing, starts with empty `Data{Version:1}` (doesn't write until the first mutation); if present, decodes **strictly** (`DisallowUnknownFields`) and runs `Validate`; on any error returns it and never writes.
  - `func (s *Store) save() error` (call with lock held): marshal with 2-space indent → `data.json.tmp` → `f.Sync()` → `os.Rename` → fsync the dir. On error, the in-memory state rolls back to the previous value (mutators work on a copy and commit only after a successful save).
  - `func (s *Store) Projects() []Project`: copy, sorted by `strings.ToLower(Name)`.
  - `func (s *Store) AddProject(name string) (Project, error)`: ID `"p_"` + 16 hex chars from `crypto/rand`; `CreatedAt = now().UTC()`.
  - `func (s *Store) RenameProject(id, name string) (Project, error)`
  - `func (s *Store) SetArchived(id string, archived bool) (Project, error)`
  - `func normalizeName(name string) (string, error)`: `strings.TrimSpace`; empty or > 100 runes → `ErrInvalid`.

- [ ] **Step 1: Write failing tests.**
  - `TestValidate`: a valid fixture passes; each of these fails with `errors.Is(err, ErrInvalid)`: version 2, empty project ID, duplicate ID, entry for unknown project, date `"2026-13-01"`, hours `0`, hours `2401`, duplicate `(projectId,date)`; `"Acme"` + `" acme "` fails with `ErrDuplicateName`.
  - `TestAddRenameArchive`: `AddProject("  Acme  ")` gives name `"Acme"`, ID matches `^p_[0-9a-f]{16}$`; `AddProject("ACME")` gives `ErrDuplicateName`; `AddProject("")` gives `ErrInvalid`; 101-rune name gives `ErrInvalid`; rename to `"Globex"` ok; renaming another project to `"globex"` gives `ErrDuplicateName`; renaming a project to its own name with different case is allowed; `SetArchived(id,true)` sets `Archived`; unknown ID gives `ErrNotFound`; `Projects()` is sorted case-insensitively.
  - `TestPersistsAcrossOpen`: add a project, `Open` the same dir again, and the project is present; `data.json.tmp` doesn't exist afterwards.
  - `TestOpenCorruptFileFailsWithoutTouchingIt`: write `{"version":1,"projects":[` to `data.json`; `Open` returns an error; the file bytes are unchanged. Repeat with a valid-JSON file that has an unknown field `"foo":1` and expect the same.
  - `TestOpenMissingFileDoesNotWrite`: `Open` on an empty dir succeeds and `data.json` doesn't exist.
- [ ] **Step 2: Run** `scripts/test.sh ./internal/store/`. Expected: FAIL, `undefined: Validate`.
- [ ] **Step 3: Implement** `model.go` then `store.go` per the Interfaces.
- [ ] **Step 4: Run** `scripts/test.sh`. Expected: PASS.
- [ ] **Step 5: Commit** `feat: data model, validation, atomic store, project operations`.

---

### Task 3: Hour entries, week view, month view

**Files:**
- Create: `internal/store/entries.go`
- Test: `internal/store/entries_test.go`

**Interfaces:**
- Consumes: `Store`, `Hours`, `ParseDate`, errors (Tasks 1–2).
- Produces:
  - `func (s *Store) SetHours(projectID, date string, h Hours) error`: project must exist (archived allowed) else `ErrNotFound`; date must parse; `h` in `0..MaxHours`; `0` deletes the entry; otherwise upsert.
  - `type WeekRow struct { ProjectID string "projectId"; Name string "name"; Archived bool "archived"; Hours [7]Hours "hours" }`
  - `type WeekView struct { Start string "start"; Days [7]string "days"; Rows []WeekRow "rows" }`
  - `func (s *Store) Week(start string) (WeekView, error)`: `start` must be a Monday (else `ErrInvalid`). Rows: every non-archived project, plus archived projects with any hours in the 7 days. Sorted like `Projects()`.
  - `type MonthRow struct { ProjectID string "projectId"; Name string "name"; Archived bool "archived"; Hours Hours "hours" }`
  - `type MonthView struct { Month string "month"; Rows []MonthRow "rows"; Total Hours "total" }`
  - `func (s *Store) Month(month string) (MonthView, error)`: `month` is strict `2006-01`. Rows: projects (any status) with total > 0 for entries whose date starts with `month + "-"`. Sorted like `Projects()`.

- [ ] **Step 1: Write failing tests.**
  - `TestSetHours`: set 4.0 on `2026-10-26`, then 6.0 on the same day overwrites; set 0 removes the entry (data has 0 entries); unknown project gives `ErrNotFound`; date `"2026-10-32"` gives `ErrInvalid`; `Hours(2401)` gives `ErrInvalid`; archived project accepts hours.
  - `TestWeekView`: projects A (active), B (archived with 2h on `2026-10-28`), C (archived, no hours that week). `Week("2026-10-26")`: `Days[0]=="2026-10-26"`, `Days[6]=="2026-11-01"`; rows are A and B only; B's `Hours[2]==200`. `Week("2026-10-27")` gives `ErrInvalid`.
  - `TestMonthSplitsYearBoundaryWeek`: A gets 8h on each of `2026-12-28`..`2027-01-03`. `Month("2026-12").Total == 3200` (4 days); `Month("2027-01").Total == 2400` (3 days).
  - `TestMonthTotalsExact`: `0.29` on three days in Oct (via `ParseHours`) gives `Month("2026-10").Rows[0].Hours.String() == "0.87"`.
  - `TestMonthIncludesArchivedAndSkipsEmpty`: an archived project with hours shows up; a project with no hours that month doesn't; `Month("2026-1")` gives `ErrInvalid`.
- [ ] **Step 2: Run** `scripts/test.sh ./internal/store/`. Expected: FAIL, `undefined: (*Store).SetHours` (or similar).
- [ ] **Step 3: Implement** `entries.go`.
- [ ] **Step 4: Run** `scripts/test.sh`. Expected: PASS.
- [ ] **Step 5: Commit** `feat: hour entries, week and month views`.

---

### Task 4: Backup download, restore, backup status

**Files:**
- Create: `internal/store/backup.go`
- Test: `internal/store/backup_test.go`

**Interfaces:**
- Consumes: `Store`, `Data`, `Validate` (Task 2).
- Produces:
  - `func DecodeBackup(raw []byte) (Data, error)`: strict decode (`DisallowUnknownFields`, reject trailing data), then `Validate`. Any failure wraps `ErrInvalid` (keep the `ErrDuplicateName` chain too).
  - `func (s *Store) ExportBackup() (raw []byte, filename string, err error)`: sets `LastBackupDownloadAt = now().UTC()`, saves, returns indented JSON of the saved data and `personal-time-tracker-backup-YYYY-MM-DD.json` (date from `now().UTC()`).
  - `func (s *Store) Restore(raw []byte) error`: `DecodeBackup` first (on error, return it with nothing written); then write the current `data.json` bytes (if the file exists) to `snapshots/pre-restore-<UTC 20060102T150405Z>.json`; then replace the data, **keeping the current `LastBackupDownloadAt`**, and save.
  - `func (s *Store) restoreData(d Data) error` (unexported; shared with Task 5): the pre-restore snapshot + replace + save part.
  - `type BackupStatus struct { LastDownloadAt *time.Time "lastDownloadAt"; Reminder bool "reminder" }`
  - `func (s *Store) BackupStatus() BackupStatus`: `Reminder = len(Projects) > 0 && (last == nil || now().Sub(*last) > 30*24*time.Hour)`.

- [ ] **Step 1: Write failing tests** (use a fake `now`):
  - `TestExportRoundTrip`: export, then `DecodeBackup(raw)` equals the store's data; filename is `personal-time-tracker-backup-2026-10-30.json` for now = 2026-10-30T12:00Z; `BackupStatus().LastDownloadAt` is set.
  - `TestRestoreReplacesAndSnapshots`: restoring a different valid backup replaces projects/entries; exactly one `snapshots/pre-restore-*.json` exists and holds the old data; `LastBackupDownloadAt` keeps the store's value, not the file's.
  - `TestRestoreRejectsBadFiles`: table of raw inputs: `not json`, `{"version":2,...}`, an entry for a missing project, duplicate `(projectId,date)`, hours `24.5`, hours `1.234`, duplicate names `"A"`/`"a"`, unknown top-level field, valid JSON followed by `{}`. Each returns an error with `errors.Is(err, ErrInvalid) || errors.Is(err, ErrDuplicateName)`, `data.json` bytes are unchanged, and no pre-restore snapshot is created.
  - `TestBackupReminder`: no projects gives false; a project and never downloaded gives true; downloaded 29 days ago gives false; 31 days ago gives true.
- [ ] **Step 2: Run** `scripts/test.sh ./internal/store/`. Expected: FAIL, `undefined: DecodeBackup`.
- [ ] **Step 3: Implement** `backup.go`.
- [ ] **Step 4: Run** `scripts/test.sh`. Expected: PASS.
- [ ] **Step 5: Commit** `feat: backup export, validated restore, backup reminder`.

---

### Task 5: Daily snapshots, listing, restore from snapshot

**Files:**
- Create: `internal/store/snapshot.go`
- Test: `internal/store/snapshot_test.go`

**Interfaces:**
- Consumes: `restoreData`, `DecodeBackup` (Task 4).
- Produces:
  - `type SnapshotInfo struct { Name string "name"; Kind string "kind"; CreatedAt time.Time "createdAt" }` with `Kind` either `"daily"` or `"pre-restore"`.
  - `var snapshotName = regexp.MustCompile(`^(daily-\d{4}-\d{2}-\d{2}|pre-restore-\d{8}T\d{6}Z)\.json$`)`
  - `func (s *Store) EnsureDailySnapshot() error`: if `data.json` exists and `snapshots/daily-<now UTC date>.json` doesn't, copy it there (write tmp + rename). Then delete dailies beyond the 14 newest (sort by name). Never deletes pre-restore files.
  - `func (s *Store) Snapshots() ([]SnapshotInfo, error)`: files matching `snapshotName`, newest first; `CreatedAt` parsed from the name (daily = that date at 00:00Z).
  - `func (s *Store) RestoreSnapshot(name string) error`: name must match `snapshotName` exactly (else `ErrInvalid`); missing file gives `ErrNotFound`; read → `DecodeBackup` → `restoreData`.

- [ ] **Step 1: Write failing tests.**
  - `TestDailySnapshotOncePerDay`: two calls on the same day leave one file; advancing `now` one day gives two; with no `data.json`, no file is made.
  - `TestDailySnapshotRotationKeeps14`: run across 20 consecutive days; 14 dailies remain, the oldest is day 7; a pre-restore file created on day 1 still exists.
  - `TestSnapshotsListing`: names, kinds, and newest-first order.
  - `TestRestoreSnapshot`: change the data after a daily snapshot, `RestoreSnapshot("daily-…json")`, and the data matches the snapshot and a pre-restore file was added.
  - `TestRestoreSnapshotRejectsBadNames`: `../data.json`, `/etc/passwd`, `daily-x.json`, `daily-2026-10-30.json/../../data.json`, `""` all return `ErrInvalid` with data unchanged; a well-formed name that doesn't exist returns `ErrNotFound`.
- [ ] **Step 2: Run** `scripts/test.sh ./internal/store/`. Expected: FAIL, `undefined: (*Store).EnsureDailySnapshot`.
- [ ] **Step 3: Implement** `snapshot.go`.
- [ ] **Step 4: Run** `scripts/test.sh`. Expected: PASS.
- [ ] **Step 5: Commit** `feat: daily snapshots with rotation and snapshot restore`.

---

### Task 6: Request guard middleware

**Files:**
- Create: `internal/web/guard.go`
- Test: `internal/web/guard_test.go`

**Interfaces:**
- Produces: `func guard(port int, next http.Handler) http.Handler`, applied in this order:
  1. Security headers on **every** response: `Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'`, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`, `Cross-Origin-Resource-Policy: same-origin`, `Cache-Control: no-store` for `/api/` paths.
  2. Host check: `strings.ToLower(r.Host)` must be one of `localhost:P`, `127.0.0.1:P`, `[::1]:P`; otherwise `403`.
  3. For methods other than GET/HEAD: require header `X-PTT-Request: 1` and `Content-Type` media type `application/json`; if an `Origin` header is present it must equal `http://` + `r.Host`. Otherwise `403`.
  4. Body limit with `http.MaxBytesReader`: 10 MiB for paths starting `/api/backup/`, 1 MiB otherwise.

- [ ] **Step 1: Write failing tests** with `httptest` against a trivial next handler (port 24871):
  - `TestGuardHost`: `localhost:24871`, `LOCALHOST:24871`, `127.0.0.1:24871`, and `[::1]:24871` get 200; `evil.com`, `localhost:8080`, `192.168.1.5:24871`, and an empty host get 403.
  - `TestGuardMutations`: a POST with header + JSON content type gets 200; a POST missing the header gets 403; a POST with `text/plain` gets 403; a POST with `Origin: http://evil.com` gets 403; a POST with `Origin: http://localhost:24871` gets 200; a GET without the header gets 200.
  - `TestGuardHeaders`: CSP and nosniff are present on a 200 and on a 403.
  - `TestGuardBodyLimit`: the next handler reads the body; 2 MiB to `/api/projects` makes the read error; 2 MiB to `/api/backup/restore` reads fine.
- [ ] **Step 2: Run** `scripts/test.sh ./internal/web/`. Expected: FAIL, `undefined: guard`.
- [ ] **Step 3: Implement** `guard.go`.
- [ ] **Step 4: Run** `scripts/test.sh`. Expected: PASS.
- [ ] **Step 5: Commit** `feat: localhost/CSRF request guard and security headers`.

---

### Task 7: JSON API, static serving, main binary

**Files:**
- Create: `internal/web/api.go`, `internal/web/static/index.html` (placeholder `<h1>Personal Time Tracker</h1>` until Task 8), `main.go`
- Test: `internal/web/api_test.go`

**Interfaces:**
- Consumes: everything in `internal/store`; `guard` (Task 6).
- Produces `func NewHandler(st *store.Store, port int) http.Handler` (the `guard`-wrapped `http.ServeMux`). Routes:

| Route | Request | Response |
|---|---|---|
| `GET /healthz` | none | `200 ok` |
| `GET /api/projects` | none | `[]Project` |
| `POST /api/projects` | `{"name"}` | `201 Project` |
| `PATCH /api/projects/{id}` | `{"name"?: string, "archived"?: bool}` (pointers; at least one) | `Project` |
| `GET /api/week?start=YYYY-MM-DD` | none | `WeekView` |
| `PUT /api/entries` | `{"projectId","date","hours"}` | `204` |
| `GET /api/month?month=YYYY-MM` | none | `MonthView` |
| `GET /api/backup/status` | none | `BackupStatus` |
| `GET /api/backup/download` | none | file body, `Content-Disposition: attachment; filename="…"` |
| `POST /api/backup/check` | backup file body | `{"projects": n, "entries": n}` (validate only) |
| `POST /api/backup/restore` | backup file body | `204` |
| `GET /api/snapshots` | none | `[]SnapshotInfo` |
| `POST /api/snapshots/{name}/restore` | `{}` | `204` |
| `GET /` and static files | none | embedded `static/` via `http.FileServerFS` |

  - Errors are JSON `{"error": "<message>"}`, checked in this order: `ErrInvalid` and JSON decode errors → 400 (so a backup file with duplicate names is a 400), `ErrNotFound` → 404, `ErrDuplicateName` → 409, `MaxBytesError` → 413, anything else → 500 (logged with `slog`, generic message).
  - Request bodies are decoded with `DisallowUnknownFields`.
- `main.go`: reads `PORT` (default `24871`) and `DATA_DIR` (default `/data`); flag `-healthcheck` makes `GET http://127.0.0.1:$PORT/healthz` with a 3s timeout and exits 0 on 200, 1 otherwise. Normal start: `store.Open(dir, time.Now)`, `EnsureDailySnapshot()` (log errors, don't exit), then call it again every hour from a ticker; `http.Server{Addr: ":"+PORT, ReadHeaderTimeout: 10s}`; graceful `Shutdown` on SIGINT/SIGTERM. Log startup to stdout with the URL `http://localhost:$PORT`.

- [ ] **Step 1: Write failing tests** in `api_test.go` (temp-dir store, `httptest.NewRecorder`, `Host: localhost:24871`, `X-PTT-Request: 1` and JSON content type on mutations):
  - `TestAPIProjectsFlow`: POST `{"name":"Acme"}` gives 201; a duplicate gives 409; PATCH `{"archived":true}` gives 200 with `archived:true`; PATCH an unknown id gives 404; PATCH `{"bogus":1}` gives 400.
  - `TestAPIEntriesWeekMonth`: PUT `{"projectId":id,"date":"2026-10-26","hours":1.5}` gives 204; GET week shows `"hours":[1.5,0,0,0,0,0,0]` for that row; GET month `2026-10` has `"total":1.5`; PUT `"hours":1.234` gives 400; PUT `"hours":"1"` gives 400.
  - `TestAPIBackupFlow`: download gives 200 with a `Content-Disposition` filename `personal-time-tracker-backup-`; that body POSTed to `/api/backup/check` gives `{"projects":1,"entries":1}`; POSTed to restore gives 204; garbage to check gives 400.
  - `TestAPISnapshotTraversal`: POST `/api/snapshots/..%2Fdata.json/restore` gives 400 or 404 and never 204; data is unchanged.
  - `TestAPIServesIndex`: GET `/` gives 200 with body containing `Personal Time Tracker` and the CSP header.
- [ ] **Step 2: Run** `scripts/test.sh ./internal/web/`. Expected: FAIL, `undefined: NewHandler`.
- [ ] **Step 3: Implement** `api.go` and `main.go`.
- [ ] **Step 4: Run** `scripts/test.sh`. Expected: PASS.
- [ ] **Step 5: Commit** `feat: JSON API, embedded static serving, main binary`.

---

### Task 8: UI shell and Week page

**Files:**
- Create/replace: `internal/web/static/index.html`, `internal/web/static/app.css`, `internal/web/static/app.js`

**Interfaces:**
- Consumes: the API in Task 7.
- Produces (for Task 9, in `app.js`):
  - `api(method, path, body?) → Promise<any>`: `fetch` with `X-PTT-Request: 1` and `Content-Type: application/json` on non-GET; throws `Error(message)` built from `{"error"}`.
  - `toHundredths(n: number) → int` (`Math.round(n*100)`), `fmtMin(h: int) → string` (minimal decimals, like Go's `Hours.String`), `fmtTotal(h: int) → string` (≥1 and ≤2 decimals: `4800→"48.0"`, `9650→"96.5"`, `1225→"12.25"`).
  - Local-date helpers: `todayLocal() → "YYYY-MM-DD"`, `mondayOf(dateStr) → dateStr`, `addDays(dateStr, n) → dateStr`. Use local `Date` parts, never `toISOString()` (UTC shifts the day).
  - Router: hash routes `#/week/YYYY-MM-DD`, `#/month/YYYY-MM`, `#/projects`, `#/backup`; empty or unknown goes to `#/week/<mondayOf(today)>`. Registers render functions `renderWeek`, `renderMonth`, `renderProjects`, `renderBackup`; Task 9 fills in the last three (this task stubs them).
  - `refreshBackupDot()`: fetches `/api/backup/status` and toggles a dot on the Backup nav item.

Requirements:
- `index.html`: no inline script or style (CSP). Left sidebar with "Personal Time Tracker" and nav links Week / Month / Projects / Backup (the current one highlighted, `aria-current="page"`); `<main id="view">`. Loads `app.css` and `app.js` (`defer`).
- Neutral styling; light and dark via `prefers-color-scheme`. Sidebar is fixed on the left; on narrow screens (< 640px) it may stack on top.
- Week page: header "Week of Mon D, YYYY" (e.g. "Week of Oct 26, 2026"), Prev / Next / "This week" controls changing the hash. Table: Project | Mon … Sun (with day-of-month) | Total; footer "Day total" row and the week total. Archived rows show "(archived)" after the name.
- Cells: `<input inputmode="decimal">` showing `fmtMin` or blank for 0. Save on `change` (fires on blur/Enter) via `PUT /api/entries`. Client-side check `^\d*\.?\d{0,2}$` and ≤ 24 before sending; on invalid input or API error, mark the cell (`.error` class + `title` with the message), don't update totals from it, and keep the typed text. On success, clear the error and recompute totals from the hundredths in memory.
- Keyboard: Enter and ArrowDown move down a row, ArrowUp moves up, ArrowLeft/ArrowRight move across when the caret is at the start/end of the text; Tab stays native.
- No active projects and no rows: show "No projects yet. Add one on the Projects page." with a link to `#/projects`.

- [ ] **Step 1: Implement** the three files to the requirements above.
- [ ] **Step 2: Run** `scripts/test.sh`. Expected: PASS (`TestAPIServesIndex` still finds `Personal Time Tracker`).
- [ ] **Step 3: Check it in the running app.** `docker run --rm -d --name ptt-dev -p 127.0.0.1:24871:24871 -e DATA_DIR=/tmp/d -v "$(pwd)":/src:ro -w /src -e CGO_ENABLED=0 -e GOTOOLCHAIN=local -e GOPROXY=off golang:1.27-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb go run .`, then `curl -s -H 'Host: localhost:24871' http://127.0.0.1:24871/app.js | head -5` returns JS and `curl -s -o /dev/null -w '%{http_code}' -H 'Host: evil.com' http://127.0.0.1:24871/` prints `403`. Look at it in a browser (via `ssh -L 24871:127.0.0.1:24871 <host>` if remote): add a project with curl, enter `1.5`, `0.25`, `abc`, and `25` in cells; totals update for valid input and the invalid cells are marked; reloading keeps the values; arrow keys move. Then `docker stop ptt-dev`.
- [ ] **Step 4: Commit** `feat: UI shell with sidebar and weekly timesheet grid`.

---

### Task 9: Month, Projects and Backup pages

**Files:**
- Modify: `internal/web/static/app.js`, `internal/web/static/app.css`

**Interfaces:**
- Consumes: `api`, `fmtTotal`, `fmtMin`, router, `refreshBackupDot` (Task 8); API (Task 7).

Requirements:
- **Month:** header "October 2026" with Prev/Next/"This month". Table Project | Hours, plus a Total footer, values shown with `fmtTotal`. Each project's hours cell and the Total cell get a copy button (`aria-label="Copy hours for <name>"`) that calls `navigator.clipboard.writeText(<exact displayed string>)` and shows "Copied" for ~1.5s; on failure it shows "Copy failed". Empty month: "No hours logged in <Month YYYY>."
- **Projects:** a form with the name input and Add (Enter submits). Active list with Rename (inline edit: input + Save/Cancel; Enter saves, Escape cancels) and Archive. "Archived" list, greyed, with Restore. Show API errors (e.g. the 409 duplicate message) inline next to the form or row. Changes re-render the list.
- **Backup:**
  - "Download backup" is a plain link to `/api/backup/download` (GET, so no custom header is needed). After clicking it, re-fetch the status and refresh the dot.
  - Show "Last backup downloaded: <local date/time>" or "Never". When `reminder` is true, show the note "It's been a while since your last backup. Download one and keep it somewhere safe (cloud drive, USB stick)."
  - "Restore from file": `<input type="file" accept=".json,application/json">`. Read the file with `FileReader`, POST the text to `/api/backup/check`, then show "This file has N projects and M hour entries. This will replace all current data. Continue?" with Continue/Cancel. Continue POSTs to `/api/backup/restore`. Show success or error inline. Validation errors from the server are shown verbatim.
  - "Automatic snapshots": list from `/api/snapshots`, showing the local date/time and kind ("Daily" / "Before restore"), each with a Restore button using the same confirm wording (without counts) → `POST /api/snapshots/<encodeURIComponent(name)>/restore` with `{}`.
- Call `refreshBackupDot()` on every route change.

- [ ] **Step 1: Implement** to the requirements.
- [ ] **Step 2: Run** `scripts/test.sh`. Expected: PASS.
- [ ] **Step 3: Check it in the running app** (same `ptt-dev` command as Task 8). Add two projects, try renaming one to a case-variant of the other and see the error inline. Archive one, check it's hidden from a week with no hours and visible in a week with hours. On the Month page, check totals across a month boundary, then copy and paste them. Download a backup, change some hours, restore the file and see the change reverted, and see a "Before restore" snapshot listed. Restoring a garbage `.json` shows an error and changes nothing. The Backup dot shows before the first download and clears after. Stop the container.
- [ ] **Step 4: Commit** `feat: month totals with copy buttons, project management, backup page`.

---

### Task 10: Container, compose, README, end-to-end verification

**Files:**
- Create: `Dockerfile`, `docker-compose.yml`, `.env`, `.dockerignore`, `README.md`

Requirements:
- **Dockerfile:**
  - Build stage `FROM golang:1.27-alpine@sha256:d200…3deb AS build` with a `# golang:1.27-alpine` comment and `ENV CGO_ENABLED=0 GOTOOLCHAIN=local GOPROXY=off GOFLAGS=-mod=readonly`.
  - Copy the source and enforce zero dependencies with `RUN test ! -e go.sum && ! grep -q '^require' go.mod`.
  - `go build -trimpath -ldflags="-s -w" -o /out/personal-time-tracker .`, then `mkdir -p /out/data`.
  - Runtime stage `FROM gcr.io/distroless/static-debian13:nonroot@sha256:18f4…2c99` with a tag comment.
  - Copy the binary, and copy `/out/data` to `/data` with `--chown=65532:65532`.
  - `ENV PORT=24871 DATA_DIR=/data`, `USER 65532:65532`, `EXPOSE 24871`, `HEALTHCHECK CMD ["/personal-time-tracker","-healthcheck"]`, `ENTRYPOINT ["/personal-time-tracker"]`.
- **docker-compose.yml:**
  - Top-level `name: personal-time-tracker`, so the volume name doesn't depend on the clone folder.
  - Service `app` with `build: .`, `image: personal-time-tracker:local`, `ports: ["127.0.0.1:${PORT:-24871}:${PORT:-24871}"]`, `environment: PORT: ${PORT:-24871}`, and volume `data:/data`.
  - `read_only: true`, `cap_drop: [ALL]`, `security_opt: ["no-new-privileges:true"]`, `restart: unless-stopped`. Top-level `volumes: data:`.
- **.env:** `PORT=24871` with a comment saying to change it here if the port is taken.
- **.dockerignore:** `.git`, `docs`, `scripts`, `*.md`.
- **README.md:** what it is; requirements (Docker Desktop or Docker Engine + Compose v2, git); install (clone → `docker compose up -d --build` → open `http://localhost:24871`); update (`git pull` → same command, data kept); change port; back up and restore (pointing at the Backup page, and saying to keep downloads off the machine); where the data lives (volume `personal-time-tracker_data`); uninstall (`docker compose down`, plus `docker compose down -v` **deletes all hours, download a backup first**); supply-chain notes (stdlib only, how to confirm no `require`/`go.sum`, `GOPROXY=off` build, digest pinning, how to bump a digest manually with `docker buildx imagetools inspect <tag>`, then edit the `@sha256:` and the comment).

- [ ] **Step 1: Write the files.**
- [ ] **Step 2: Build and start.** `docker compose up -d --build`. Expected: build succeeds (proving no downloads under `GOPROXY=off`); `docker compose ps` shows `healthy` within ~30s.
- [ ] **Step 3: Verify the lockdown.**
  - `docker compose config --volumes` lists `data`.
  - `docker volume ls | grep personal-time-tracker_data` matches.
  - `docker port personal-time-tracker-app-1` shows `127.0.0.1:24871`.
  - `docker inspect -f '{{.Config.User}} {{.HostConfig.ReadonlyRootfs}} {{.HostConfig.CapDrop}}' personal-time-tracker-app-1` prints `65532:65532 true [ALL]`.
- [ ] **Step 4: Verify persistence.** Create a project and an entry with curl (`-H 'Host: localhost:24871' -H 'X-PTT-Request: 1' -H 'Content-Type: application/json'`), then run `docker compose down && docker compose up -d --build`. `GET /api/month?month=<entry month>` still shows the hours. Clone or copy the repo into a different folder name, run `docker compose up -d` there, and it sees the same data. Clean up the copy and run `docker compose down` (no `-v`).
- [ ] **Step 5: Run** `scripts/test.sh`. Expected: PASS.
- [ ] **Step 6: Commit** `feat: hardened container, compose, README`.
- [ ] **Step 7: Owner's walkthrough.** Ask the owner to open the app at `http://localhost:24871` and use it for a few minutes before handing it to the team.
