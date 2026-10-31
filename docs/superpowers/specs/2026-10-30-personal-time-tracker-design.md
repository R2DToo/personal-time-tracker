# Personal Time Tracker — Design

Date: 2026-10-30
Status: Approved

## Why this exists

Consultants who bill by monthly invoice need a simple way to track their own hours, without paying for a hosted time-tracking service whose extra features they don't use. There's no central system: each consultant keeps their own records, and only the invoices they send leave their hands.

Personal Time Tracker is a small app for those consultants. Each consultant runs it in Docker on their own computer, so it costs nothing to host. They log hours per project every day or once a week. At month end they look up total hours per project for that month and copy those numbers into their invoice.

## Goals

- Log decimal hours against projects for each day, using a weekly timesheet grid.
- Create, rename, archive, and restore projects.
- Show total hours per project for any calendar month, with one-click copy of each number to the clipboard.
- Keep data across container restarts, rebuilds, and updates by storing it in a Docker volume.
- Protect against data loss with a downloadable backup file, restore from that file, and automatic daily snapshots.
- Run on Windows, Intel Mac, Apple Silicon Mac, and Linux under Docker / Docker Desktop.
- Be hardened against supply-chain attacks (see Constraints).

## Non-goals

- More than one user per install: no logins, accounts, or user switching.
- Any sharing, syncing, or reporting to anyone else. Only the invoices leave the consultant's computer.
- Hourly rates, money amounts, notes or descriptions, clients as a separate concept, tags, or billable/non-billable flags.
- Generating invoices.
- CSV or spreadsheet export (considered and explicitly declined; the clipboard buttons replace it).
- Start/stop timers. Hours are typed in.
- Deleting projects. Archive is the way to retire one.
- Mobile app or mobile-first layout. It's a desktop browser app; it only needs to be usable, not polished, on narrow screens.

## Anti-goals (would make this a failure even if it works)

- Any third-party code in the app or build: no npm, no Go modules beyond the standard library, no CDN-loaded scripts, styles, or fonts.
- The app being reachable from anything other than the consultant's own computer.
- Silently losing or corrupting hours. A failed save must be visible to the user, and a bad restore file must never wipe existing data.

## Constraints

### Language and dependencies (Claude's call, owner agreed)

- **Go, standard library only.** `go.mod` has no `require` lines and the repo has no `go.sum`. That's checkable, and the README should say so.
- **Frontend:** plain HTML, CSS, and vanilla JavaScript, served from files embedded in the binary with `embed`. No build step, no bundler, no external assets.
- **Storage:** one JSON file in the data volume (not SQLite, which would need a third-party driver). Every save writes a temp file in the same directory, fsyncs it, then atomically renames it over the real file. A mutex in the server serializes writes.

### Container

- Multi-stage Dockerfile. The build stage is an official Go image; the runtime stage is minimal (`gcr.io/distroless/static-debian12:nonroot` or `scratch`, builder's call). **Both are pinned by `@sha256:` digest**, with the human-readable tag in a comment.
- Static binary: `CGO_ENABLED=0`, built with `-trimpath`.
- Runs as a non-root user (UID 65532 or similar). The data directory (e.g. `/data`) must exist in the image owned by that user, so a fresh named volume picks up the right ownership on every platform.
- `docker-compose.yml`:
  - builds from the local Dockerfile (no published image);
  - named volume mounted at the data directory;
  - port published as `127.0.0.1:<port>:<port>`, default 24871 (chosen to avoid collisions: unregistered, and below the OS ephemeral port ranges — Linux 32768+, Windows/macOS 49152+), easy to change in one place;
  - `read_only: true`, `cap_drop: [ALL]`, `security_opt: [no-new-privileges:true]`, `restart: unless-stopped`.
- Image updates are manual. The README explains how to bump a pinned digest. No automated update tooling.

### Web security (app is localhost-only, but browsers visit hostile sites)

- Reject requests whose `Host` header isn't `localhost`/`127.0.0.1` with the configured port, to block DNS-rebinding attacks.
- State-changing API calls require a JSON body and a custom request header (e.g. `X-Requested-With`) so a malicious web page can't forge them with a plain form post. Send no CORS headers.
- Set a strict `Content-Security-Policy` (self only, no inline scripts) and basic hardening headers.
- Cap request body sizes, including restore uploads.

## Distribution

- Code lives in a GitHub repo the consultants clone.
- Install: clone the repo, then `docker compose up -d --build`, then open `http://localhost:24871`.
- Update: `git pull`, then `docker compose up -d --build`. The data volume is untouched.
- Images are built locally on each machine, so every CPU architecture works with no registry or credentials.
- README: install, update, change port, back up and restore, uninstall (including how to remove the volume, with a warning), and the supply-chain notes above.

## The app

### Layout

A fixed **left sidebar** (the owner chose this over tabs) with the app name "Personal Time Tracker" and four items: **Week**, **Month**, **Projects**, **Backup**. The current page is highlighted. The main content sits to the right. Mockups from the brainstorm are below; they show structure, not final styling.

```
┌──────────────────┬───────────────────────────────────────────────────────────┐
│ Personal Time    │  ← Prev        Week of Oct 26, 2026            Next →     │
│ Tracker          │  ┌──────────────────┬───┬───┬───┬───┬───┬───┬───┬──────┐  │
│                  │  │ Project          │Mon│Tue│Wed│Thu│Fri│Sat│Sun│Total │  │
│ ▌Week            │  ├──────────────────┼───┼───┼───┼───┼───┼───┼───┼──────┤  │
│  Month           │  │ Acme ERP Migr.   │ 4 │ 6 │3.5│ 8 │   │   │   │ 21.5 │  │
│  Projects        │  │ Globex Audit     │ 4 │ 2 │4.5│   │   │   │   │ 10.5 │  │
│  Backup •        │  ├──────────────────┼───┼───┼───┼───┼───┼───┼───┼──────┤  │
│                  │  │ Day total        │ 8 │ 8 │ 8 │ 8 │ 0 │ 0 │ 0 │  32  │  │
└──────────────────┴───────────────────────────────────────────────────────────┘
```

### Week page (home)

- Opens on the current week, based on the browser's local date. **Weeks run Monday to Sunday.**
- Header shows "Week of <Monday's date>" with Prev/Next arrows, plus a way to jump back to the current week.
- One row per project, ordered by name. A row shows if the project is active, **or** if it's archived but has hours in this week (so past weeks still display correctly).
- Cells take **decimal hours** (`1.5`, `0.25`). Not `h:mm`.
- Validation: a number from 0 to 24 with at most two decimal places. Empty or 0 means no entry. Invalid input gets flagged inline and isn't saved.
- **Autosave.** A cell saves when it changes (on blur/Enter). No save button. A save failure is shown clearly on the cell and isn't silently dropped. Tab and arrow keys move between cells like a spreadsheet *(Claude's call)*.
- Row totals, day totals, and a week total update live.
- If there are no active projects, show a prompt linking to the Projects page.

### Month page

- Pick a calendar month with Prev/Next arrows. Defaults to the current month.
- Lists every project (active or archived) with more than 0 hours in that month, with its total and a grand total.
- **A week that spans two months splits by day.** Each month counts only its own days.
- **Copy buttons:** each project's total and the grand total have a small copy button that puts just the number (e.g. `96.5`) on the clipboard and briefly shows "Copied". Uses the Clipboard API, available because `localhost` is a secure context.
- Number formatting is consistent (e.g. one decimal place minimum, two max). The copied value matches what's displayed.

### Projects page

- Add a project by name (trimmed, non-empty, unique ignoring case).
- Each active project has **Rename** and **Archive**. Archived projects are listed separately (greyed) with **Restore**.
- Archiving hides the project from the Week grid going forward but keeps all its hours; it still appears on Month totals for months where it has hours.
- Renaming changes the name everywhere, history included. Hours are tied to a stable project ID, not the name.

### Backup page

- **Download backup:** downloads one JSON file named like `personal-time-tracker-backup-2026-10-30.json` with all projects (archived included) and all hour entries. The file has a format version number.
- **Restore from file:** upload a backup file. The server fully validates it (format version, structure, value ranges, unique IDs/names) before changing anything. Then it asks "This will replace all current data. Continue?" On confirm, it first saves the current data as a pre-restore snapshot, then replaces it.
- **Automatic snapshots:** once per day, the first time the app is running on a new day *(Claude's call on the trigger)*, it copies the data file into a snapshots folder in the volume. It keeps the 14 most recent. Pre-restore snapshots are kept too and labelled as such. Snapshots are listed on this page with date/time and can be restored with the same confirm-and-save-current flow.
- **Reminder:** if there's data and no backup has been downloaded in 30+ days (or ever), show a small dot on the Backup sidebar item and a short note on the Backup page. Downloading a backup records the time and clears it.

## Data model (sketch; builder may refine)

```json
{
  "version": 1,
  "projects": [
    { "id": "p_…", "name": "Acme ERP Migration", "archived": false, "createdAt": "…" }
  ],
  "entries": [
    { "projectId": "p_…", "date": "2026-10-26", "hours": 4 }
  ],
  "lastBackupDownloadAt": "…or null"
}
```

- At most one entry per (project, date). Setting hours to empty or 0 removes it.
- Dates are plain calendar dates (`YYYY-MM-DD`) with no time zone; the browser decides which day "today" is.
- Store hours so that sums don't pick up float noise (e.g. hundredths as integers, or round on output). Builder's call; displayed totals must be exact.

## Testing expectations

- Go standard `testing` only. Cover: storage round-trip and atomic save, entry upsert/delete, validation, month aggregation including weeks that cross month boundaries, archived-project visibility rules, backup validation (rejects bad files without touching data), snapshot rotation, and the Host/CSRF checks.
- Build and run the container for real and check that data survives `docker compose down` / `up --build`.

## Settled details (Claude's calls, from design review)

- Archived projects that show on the Week grid (because they have hours that week) stay editable.
- Pre-restore snapshots are all kept, outside the 14-day rotation; they're tiny.
- Restoring a backup keeps the current `lastBackupDownloadAt` and doesn't bring back the file's value.
- One `PORT` setting (in a `.env` next to the compose file, default 24871) drives both sides of the `127.0.0.1:<port>:<port>` mapping and is passed to the app for the Host-header check. Allowed hosts are `localhost`, `127.0.0.1`, and `[::1]` on that port.
- The server stores timestamps in UTC; the browser shows them in the user's local time.

## Left to the builder

- Exact visual styling (clean and neutral; light and dark mode via `prefers-color-scheme` would be nice).
- API shape, file layout in the repo, ID format, how hours are stored internally.
- Distroless vs scratch, and whether there's a container healthcheck (e.g. a `-healthcheck` flag on the binary, since the runtime image has no curl).
- Exact Go and base-image versions (current stable, pinned by digest).

## Decisions log

**Owner decided:** single-user, per-machine Docker install; month-end view is total hours per project only (no rates, no notes); weekly grid entry; left sidebar instead of tabs; projects can be renamed and archived; decimal hours; months split weeks by day; weeks start Monday; backup download/restore, daily snapshots, and a backup reminder; no CSV export; copy-to-clipboard buttons instead; name "Personal Time Tracker"; distributed via a GitHub repo.

**Claude's calls:** Go standard library + vanilla JS with zero third-party dependencies; JSON file storage with atomic writes; build images locally rather than publishing to a registry; digest-pinned base images; localhost-only port binding and container lockdown; Host-header and custom-header checks; copy buttons live on the Month page (project totals and grand total); 14 snapshots retained; 30-day reminder threshold; spreadsheet-style keyboard movement in the grid; no project deletion.
