# Personal Time Tracker

A small, private timesheet you run on your own computer. Log hours against
your projects for each day of the week, then check each project's monthly
total when you write your invoice.

- One user, one computer. Nothing is shared or synced, and nobody else can see it.
- It runs in Docker, and your hours are kept in a Docker volume, so they survive restarts and updates.
- Only your own computer can reach it. It's never exposed to your network.

## Requirements

- [Docker Desktop](https://www.docker.com/products/docker-desktop/) (Windows or Mac) or Docker Engine with the Compose plugin (Linux)
- `git`

## Install

```sh
git clone https://github.com/R2DToo/personal-time-tracker.git
cd personal-time-tracker
docker compose up -d --build
```

Open **http://localhost:24871** in your browser.

It starts again automatically whenever Docker starts.

## Using it

- **Week**: type hours as decimals (`1.5`, `0.25`) into the grid. Each cell saves when you leave it or press Enter. Arrow keys, Enter and Tab move between cells.
- **Month**: shows total hours per project for the month. Use the **Copy** buttons to paste the numbers into your invoice. A week that spans two months is split by day.
- **Projects**: add, rename, archive and restore projects. Archived projects disappear from the week grid but keep their hours.
- **Backup**: download a backup file, restore from one, or roll back to an automatic snapshot.

## Update

```sh
cd personal-time-tracker
git pull
docker compose up -d --build
```

Your data is untouched by updates.

## Change the port

If port 24871 is already used on your computer, edit `PORT` in `.env` and run
`docker compose up -d` again. Then open `http://localhost:<new port>`.

## Back up and restore

Your hours exist only on this computer. Use the **Backup** page to download a
backup file regularly (the month-end invoice is a good moment), and keep it
**off this computer**: a cloud drive, a USB stick, anywhere that survives a dead
laptop. A red dot appears on **Backup** if you haven't downloaded one in 30 days.

To move to a new computer, install the app there and use **Restore from file**.

The app also keeps a daily snapshot inside the volume (the last 14 days),
plus a copy of your data before every restore. You can roll back to any of
them from the Backup page. These protect against mistakes, not against
losing the computer.

## Troubleshooting

**The page won't load ("site can't be reached").** Check the app's log:

```sh
docker compose logs --tail 20 app
```

If it says `data.json is unreadable`, the data file was damaged (or edited by
hand) and the app refuses to start rather than risk losing your hours. Recover
from one of the automatic snapshots:

```sh
docker compose run --rm app -list-snapshots          # newest first
docker compose run --rm app -recover-snapshot daily-2026-10-30.json
docker compose restart
```

The damaged file is kept aside next to it (`data.json.broken-<time>`), and
nothing changes if the snapshot itself is invalid. If you'd rather restore a
downloaded backup, recover any snapshot first so the app starts, then use
**Restore from file** on the Backup page.

## Where the data lives

In the Docker volume `personal-time-tracker_data`, as `data.json` plus a
`snapshots/` folder.

## Uninstall

```sh
docker compose down        # stops and removes the container; keeps your data
docker compose down -v     # ALSO DELETES ALL YOUR HOURS. Download a backup first.
```

## Security and supply chain

This app is built to have no third-party code at all:

- **Go standard library only.** `go.mod` has no `require` lines and there is no `go.sum`. Check with:
  ```sh
  grep -c '^require' go.mod   # prints 0
  ls go.sum                   # "No such file or directory"
  ```
  The Docker build fails if either ever appears.
- **No downloads during the build.** It runs with `GOPROXY=off` and `GOTOOLCHAIN=local`, so Go can't fetch any module or toolchain.
- **No npm, no frameworks, no CDNs.** The web page is plain HTML, CSS and JavaScript built into the binary.
- **Pinned base images.** Both images in the `Dockerfile` are pinned by `@sha256:` digest, so they can't change without someone editing the file.
- **Locked-down container.** It runs as a non-root user, with a read-only filesystem except the data volume, all Linux capabilities dropped, and `no-new-privileges`. The port is bound to `127.0.0.1`.
- **Browser protections.** The app rejects requests that don't name `localhost` (blocking DNS-rebinding tricks), refuses API requests that browsers mark as coming from another site, requires a custom header and JSON body for every change, and sends a strict Content-Security-Policy.

### Updating a pinned base image

Updates are manual on purpose. To move to a newer image:

```sh
docker buildx imagetools inspect golang:1.27-alpine
docker buildx imagetools inspect gcr.io/distroless/static-debian13:nonroot
```

Copy the `Digest:` value into the matching `FROM ...@sha256:` line in the
`Dockerfile` (and `scripts/test.sh` for the Go image), update the comment
above it if the tag changed, then rebuild and run the tests.

## Development

Go doesn't need to be installed. Tests run inside the same pinned Go image:

```sh
scripts/test.sh                                   # vet + all tests
scripts/test.sh -run TestMonth ./internal/store/  # a subset
```
