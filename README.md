# Yomi Reader

A simple, low-memory manga / webtoon reader for a folder-based library — the kind of layout apps
like [Suwayomi](https://github.com/Suwayomi/Suwayomi-Server) keep on disk. Point it at a tree of
sources/series/chapters and read. Self-hosted in Go (no CGO); the library stays **read-only**.

## Features

* **Home / Adult tabs** — X-rated series live on a separate Adult home so they never mix with
  the main shelves (E/T/M). Only users with an X age cap (or admins) see the tab; Browse stays
  a full catalog with optional age filters.
* **Multi-source merge** — same title across Suwayomi-style source folders becomes one series;
  chapters merge by number with source priority and official/unofficial edition picking.
* **Per-user limits** — age caps (`E`/`T`/`M`/`X`) and library ACL, enforced server-side.
* **Webtoon-first reader** — vertical scroll with scroll-position resume, plus LTR/RTL/vertical
  modes, per-series mode, PDF embed, and alternate source/edition in the reader.
* **Low footprint** — Go + SQLite, no CGO; library stays read-only; pages stream from disk;
  only a small cover cache is written.
* **Plex / local / mock auth** — Plex popup PIN by default (no public callback); local passwords
  for OPDS Basic; mock for dev.
* **OPDS 1.2**, installable **PWA** (EN/FR SPA, no frontend build), incremental + watched scan,
  ComicInfo metadata, Synology-friendly Docker releases.

See [TODO.md](TODO.md) for planned work.

## Quick start

```powershell
# Windows — Plex machine ID from Settings → General, or /identity
$env:PLEX_MACHINE_ID = "yourMachineId"
go build -o yomi-reader.exe .
.\yomi-reader.exe
# -> http://localhost:8098
```

```bash
# Dev (auto-login, custom library)
AUTH_PROVIDER=mock DEFAULT_LIBRARY_PATH=/path/to/suwayomi DATA_PATH=./testdata/data go run .
```

Docker: copy `.env.example` → `.env`, set `PLEX_MACHINE_ID`, edit the library mount in
`docker-compose.yml`, then `docker compose up -d`. For local auth instead:
`docker compose -f docker-compose.yml -f docker-compose.local.yml up -d`.

Plex sign-in uses a browser popup (no public callback URL). Shared Plex users can log in; the
server owner becomes admin. OPDS Basic auth needs a **local** account.

## Releases (Synology / Docker)

```bash
git tag -a v0.1.0 -m "Release v0.1.0"
git push origin v0.1.0
```

GitHub Actions publishes multi-arch images to GHCR and attaches offline tarballs to the release.

| Artifact | Use |
|---|---|
| `ghcr.io/angedelamort/yomi-reader:latest` | `docker pull` |
| `yomi-reader-vX.Y.Z.tar.gz` | pinned offline image |
| `yomi-reader-latest.tar.gz` | **Synology** — Container Manager → Image → Add from file |

Local build: `make release VERSION=v0.1.0` (Windows: `.\scripts\release.ps1 -Version v0.1.0`).

On Synology, import **`yomi-reader-latest.tar.gz`** so the tag is `angedelamort/yomi-reader:latest`.
Mount `/data` (read-write) and `/manga` (library, read-only). Set `PUID`/`PGID` if needed.

## Configuration

| Variable | Default | Notes |
|---|---|---|
| `DATA_PATH` | `./data` | DB, session key, cover cache |
| `PORT` | `8098` | |
| `AUTH_PROVIDER` | `plex` | `plex`, `local`, or `mock` |
| `COOKIE_SECURE` | `false` | `true` behind HTTPS |
| `SESSION_SECRET` | auto | stored in `DATA_PATH/session.key` if unset |
| `ADMIN_USER` / `ADMIN_PASS` | – | local provider first admin (`admin`/`admin` if empty DB) |
| `PLEX_MACHINE_ID` | – | required for plex auth |
| `PUBLIC_BASE_URL` | – | optional Plex redirect base |
| `DEFAULT_LIBRARY_PATH` | – | auto-registered on first run |
| `AUTOSCAN_WATCH` | `true` | watch library folders for new chapters |
| `THUMB_WIDTH` | `300` | cover width |
| `PUID` / `PGID` | `1000` / `1000` | Docker: ownership of `/data` |
| `TZ` | `America/Toronto` | container timezone |

## Library layout

```
<root>/mangas/{Source}/{Series}/{Chapter.cbz|.zip|.epub|.pdf|image-folder/}
```

If `<root>/mangas` is missing, `<root>` itself is used.

* Series with the same normalized title across sources are merged into one.
* Chapter numbers are parsed from filenames (`Chapter 11.5`, `Ch.006`, `Vol.6 Ch.238`, `#001`, …).
* `ComicInfo.xml` fills empty metadata; age ratings (`E`/`T`/`M`/`X`) come from ComicInfo, genres, or source name.
* Best source wins per chapter number (admin / per-series source priority); editions like `official_` /
  `unofficial_` collapse to one entry.
* Scans are incremental. Auto-scan watches source/series folders (pair with Admin → `auto_scan_hours`
  on Docker/network mounts). Unsupported: `.cbr` / `.rar` / `.cb7` / `.7z`.

## API & OPDS

JSON under `/api/*` (auth, catalog, progress, favorites, admin). Errors are `{"error":"..."}`.
OPDS 1.2 at `/opds` (session cookie or HTTP Basic with a local account). `GET /healthz` is open.

Run `go test ./...` for parser, merge, path, and archive checks.
