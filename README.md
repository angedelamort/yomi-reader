# Yomi Reader

Self-hosted manga / webtoon reader in Go (no CGO): SQLite catalog, multi-source series merging,
a fast webtoon-first SPA, reading progress, per-user age/library restrictions, OPDS, and admin.

The library is **read-only**: nothing is ever written into a library root. Covers are cached under
`DATA_PATH/cache/covers`.

## UI

Vanilla SPA (no build step) with EN/FR:

* **Home** — continue reading, favorites, recently added shelves
* **Library** — cover grid, search, tags, source/age filters, sort, infinite scroll
* **Series** — merged chapter list across sources (A then B fallback), resume, **favorites** (series-only)
* **Reader** — webtoon vertical scroll with page preload, plus LTR/RTL/vertical page modes; PDF embed
* **Admin** — libraries + folder browser + scan, users/permissions/age caps, source priority, jobs
* **PWA** — installable (`manifest.webmanifest` + service worker): app shell offline, cached covers,
  recently viewed pages; install from the browser on phone/desktop (HTTPS or localhost)

Auth: **plex** (default, plex.tv PIN popup), **local** (username/password), or **mock** (dev).

See [TODO.md](TODO.md) for planned work (new-chapter alerts, reading lists, metadata providers, …).

## Quick start

```powershell
# Windows — set your Plex machine ID first (Settings → General, or /identity).
$env:PLEX_MACHINE_ID = "yourMachineId"
go build -o yomi-reader.exe .
.\yomi-reader.exe
# -> http://localhost:8098   Sign in with Plex
```

```bash
# Dev with auto-login (no password), custom library
AUTH_PROVIDER=mock DEFAULT_LIBRARY_PATH=/path/to/suwayomi DATA_PATH=./testdata/data go run .
```

Docker: copy `.env.example` → `.env`, set `PLEX_MACHINE_ID`, then `docker compose up -d`
(edit the library mount in `docker-compose.yml`). For username/password instead:
`docker compose -f docker-compose.yml -f docker-compose.local.yml up -d`.

## Releases (Synology / Docker)

Tag a version and push — GitHub Actions builds multi-arch images on GHCR and attaches
offline tarballs to the GitHub Release:

```bash
git tag -a v0.1.0 -m "Release v0.1.0"
git push origin v0.1.0
```

| Artifact | Use |
|---|---|
| `ghcr.io/angedelamort/yomi-reader:latest` | `docker pull` on a host with registry access |
| `yomi-reader-vX.Y.Z.tar.gz` | pinned offline image |
| `yomi-reader-latest.tar.gz` | **Synology**: Container Manager → Image → Add → Add from file |

Local build (same dual-tarball layout as CI):

```bash
make release VERSION=v0.1.0
# Windows: .\scripts\release.ps1 -Version v0.1.0
```

On Synology, import **`yomi-reader-latest.tar.gz`** so the RepoTag is exactly
`angedelamort/yomi-reader:latest` (that is what Container Manager matches on update).
Mount `/data` (read-write) and `/manga` (library, read-only). Set `PUID`/`PGID` to your
DSM share owner if needed.

## Configuration (env)

| Variable | Default | Notes |
|---|---|---|
| `DATA_PATH` | `./data` | SQLite DB (`yomi-reader.db`), `session.key`, `cache/covers/` |
| `PORT` | `8098` | |
| `AUTH_PROVIDER` | `plex` | `plex` (plex.tv PIN), `local` (bcrypt), or `mock` |
| `COOKIE_SECURE` | `false` | `true` only behind HTTPS |
| `SESSION_SECRET` | auto | otherwise generated and stored in `DATA_PATH/session.key` |
| `ADMIN_USER` / `ADMIN_PASS` | – | local provider: first admin; empty DB without them creates `admin`/`admin` |
| `PLEX_MACHINE_ID` | – | **required** for default plex auth (PMS machineIdentifier) |
| `PUBLIC_BASE_URL` | – | optional; base URL for Plex redirect callback (`/auth/login`) |
| `MOCK_USER` / `MOCK_ADMIN` | `dev` / `true` | mock provider identity |
| `THUMB_WIDTH` | `300` | cover width (height = 1.5 x) |
| `DEFAULT_LIBRARY_PATH` | – | auto-registered on first run; else `M:\books\suwayomi` if present |
| `AUTOSCAN_WATCH` | `true` | fsnotify watcher (see Auto-scan below) |
| `STATIC_DIR` | `static` | SPA directory; unknown paths fall back to `index.html` |
| `PUID` / `PGID` | `1000` / `1000` | Docker only: ownership of `/data` (linuxserver-style) |
| `TZ` | `America/Toronto` | timezone inside the container |

Session cookie: `yr_session` (HMAC-SHA256 signed, 30 days, HttpOnly, SameSite=Lax).
The admin flag is re-read from the DB on every request, so demoting/deleting a user takes effect immediately.

## Library layout and scanning

```
<root>/mangas/{Source}/{Series}/{Chapter.cbz | .zip | .epub | .pdf | chapter-folder-of-images/}
```

If `<root>/mangas` exists it is used, otherwise `<root>` itself is the mangas root.

* Every source folder becomes a **source**; every series folder is upserted by **normalized title**
  (lowercase letters/digits, punctuation dropped), so the same series in several sources merges into one.
* Chapter numbers are parsed from filenames: `Chapter 11.5`, `Ch.006.1`, `Vol.6 Ch.238`, `#001`, `MANGA Plus_#001 - ...`.
  Unparseable names get number `-1` (never merged, listed last).
* `ComicInfo.xml` (folder or inside the CBZ) fills series summary/authors/status/genres/tags when empty
  (the Tachiyomi/Suwayomi `PublishingStatusTachiyomi` field is understood; its `Year` is a chapter date and ignored).
* Age rating (`E` < `T` < `M` < `X`): `X` for sources containing hentai/porn/nsfw, genres Adult/Smut/Hentai/Erotica,
  or when most files start with `# `; `M` for Ecchi/Seinen/Mature/Gore; default `T`; `E` for Kids/Children. ComicInfo
  `AgeRating` is honoured; an incremental scan never loosens an existing rating. Admins can edit metadata
  (title, summary, authors, status, year, genres, tags, age, source priority) via **Edit** on the series page /
  `PUT /api/admin/series/{id}`; saving locks fields against ComicInfo fills (`metaLocked`).
* Rescans are incremental (size + mtime), inspect archives concurrently, and are single-flight per library.
  Deletions only happen when the listing was complete. `.cbr/.rar/.cb7/.7z` are counted as unsupported and skipped.
* **Auto-scan:** `AUTOSCAN_WATCH` (default on) watches library root, source, and series folders (fsnotify is not
  recursive; chapter image folders are not watched). New chapter archives dropped into a series folder trigger a
  rescan after a 20 s debounce. Pair it with Admin → `auto_scan_hours` (e.g. 6–12) as a safety net — fsnotify is
  often unreliable on Docker binds, mapped drives, and network mounts. There is no user-facing “new chapter” alert
  yet (watcher only updates the catalog); see [TODO.md](TODO.md).
* Covers: first page of the lowest chapter, cropped to 2:3 (webtoon strips cropped at the top), generated in the
  background (and lazily on first request). PDF-only series get a placeholder. Admins can override a cover
  (upload, pick a chapter’s first page, or reset to auto) via `PUT /api/admin/series/{id}/cover`; overrides are
  locked against rescans (`coverLocked`).
* Paths are always checked to stay under the library root before opening a chapter.

**Merged chapters:** for each chapter number the copy from the best source wins (lower priority = better;
then **edition**: `official_` filename prefix beats plain/`unofficial_`; then more pages). Same-source
pairs like MangaFire’s `official_Ch. N` / `unofficial_Ch. N` collapse to one reading entry (official by
default; unofficial only when official is missing); the other edition stays in `alternatives`. Prefer
per-series order via **Edit → Sources** (`sourcePriority` on the series; only shown when a title has more
than one source). The admin `source_priority` setting is the default for series without an override
(CSV of source names, `MangaDex` matches `MangaDex (EN)`).

## API (JSON)

Errors are `{"error": "..."}`. Ages/ratings use codes `E`, `T`, `M`, `X`.

**Auth** – `GET /api/auth/info` → `{provider, authenticated, user, version}`; `POST /api/auth/login {username,password}`
(local only); `POST /api/auth/logout`; `GET /auth/login|/auth/callback|/auth/logout` (redirect / mock / Plex
fallback). Plex popup: `GET /api/auth/plex/config`, `POST /api/auth/plex/exchange {token}` — users with access to
`PLEX_MACHINE_ID` may sign in; the Plex server **owner** is admin. Failed password logins are throttled
(8 / 10 min per IP+user).

**User** – `GET /api/me`; `GET|PUT /api/preferences` (free-form JSON object, e.g. `lang`); `GET /api/libraries`.

**Catalog**
* `GET /api/home` → `{continue, favorites, recentlyAdded, recentlyUpdated, random}`
  (`continue` items: `{series, chapterId, chapterNumber, chapterTitle, pageIndex, scrollRatio}`; finished series are dropped,
  a completed chapter points to the next one). `random` is a stable sample for the configured refresh window.
* `GET /api/series?q=&tag=&source=&age=&sort=&order=&offset=&limit=&library=&favorites=1`
  → `{items,total,offset,limit}`. `tag` is repeatable / CSV and matches genres or tags (AND); `age` caps the rating;
  `sort` ∈ title|added|updated|chapters|year|random, `order` asc|desc; `limit` ≤ 200 (default 48).
* `GET /api/series/{id}` → series + `progress`, `resume`, `readerMode`, `sourceDetails`, `firstChapterId`, `lastChapterId`.
  `readerMode` is the user's per-series mode (ltr|rtl|vertical|webtoon), falling back to the admin default.
* `PUT /api/series/{id}/reader-mode` `{readerMode}` – pin the reading mode for that series.
* `GET /api/series/{id}/chapters` – merged (`alternatives` lists other sources); `?allSources=1` – every copy;
  `?group=volume` – `[{volume, chapters}]`.
* `GET /api/chapters/{id}` – chapter + `prev`/`next`; `GET /api/chapters/{id}/pages` → `[{index,name}]`;
  `GET /api/chapters/{id}/page/{n}` – image bytes (`Cache-Control: private, max-age=86400`, ETag; for PDF, page 0 is the PDF);
  `GET /api/chapters/{id}/file` – raw cbz/epub/pdf with Range (folders are streamed as a CBZ).
* `GET /api/covers/{seriesId}`, `GET /api/tags` (`[{name,count}]`), `GET /api/sources`.

PDF chapters have `format: "pdf"` and one pseudo-page: embed `/api/chapters/{id}/file` in the viewer. EPUB uses spine/manifest
images (image-only EPUBs only).

**Progress / favorites** – `GET|PUT|DELETE /api/progress/{seriesId}` (PUT body `{chapterId,pageIndex,scrollRatio,completed}`;
response `{progress, resume}` where `resume` is the chapter/page to open). Favorites are **series-only**:
`GET /api/favorites`, `PUT|DELETE /api/favorites/{seriesId}` (Home shelf + series page; not per-chapter).

**Admin** (`isAdmin` only)
* Libraries: `GET|POST /api/admin/libraries`, `PUT|DELETE /api/admin/libraries/{id}`,
  `POST /api/admin/libraries/{id}/scan` (202), `GET /api/admin/libraries/{id}/scan-progress`, `GET /api/admin/browse?path=`
  (drive letters on Windows when empty).
* Users: `GET|POST /api/admin/users`, `PUT|DELETE /api/admin/users/{username}` – fields `password, email, isAdmin,
  maxAgeRating (E|T|M|X), libraries[]`. Non-admins see only whitelisted libraries and series up to their `maxAgeRating`
  (new users default to `T`; the DB default is `E`).
* Settings: `GET|PUT /api/admin/settings` – `source_priority` (CSV), `auto_scan_hours` (0 = off),
  `default_reader_mode` (fallback when a series has no per-user mode yet),
  `random_refresh_minutes` ∈ 30|60|360|720|1440 (home Random lane stability; default 360 / 6h).
* Also: `GET /api/admin/sources`, `GET /api/admin/jobs`,
  `PUT /api/admin/series/{id}` (metadata + `ageRating` / `metaLocked` / `sourcePriority`),
  `PUT /api/admin/series/{id}/cover` (multipart `file`, or JSON `{"chapterId"}` / `{"auto":true}`).

**OPDS 1.2 (+ page streaming)** – `GET /opds`, `/opds/browse?q=&sort=&page=`, `/opds/series/{id}`, `/opds/search.xml`,
`/opds/covers/{id}`, `/opds/chapters/{id}/file`, `/opds/chapters/{id}/page/{n}`. Auth: session cookie **or HTTP Basic**
with a local account. Same library/age restrictions as the API.

`GET /healthz` is unauthenticated.

## Layout

```
main.go             wiring, bootstrap admin + default library
config/             env configuration
auth/               session cookies (yr_session), mock + local providers, middleware
library/            db, model, store, scanner, archive (cbz/zip/epub/pdf/folder), cover, merge,
                    handler (+ admin.go, opds.go), autoscan, comicinfo, util
server/             routes, login rate limit, static SPA + fallback
static/             SPA (index.html, app.js, style.css, i18n.js, plex-login.js, sw.js, manifest, icons/)
```

### Plex auth (default)

```bash
PLEX_MACHINE_ID=yourMachineId go run .
# or: docker compose up -d   # after setting PLEX_MACHINE_ID in .env
```

Sign-in uses a browser popup against plex.tv (no public callback URL required). Shared Plex users can
log in; the server owner becomes Yomi admin. After first login, tune age caps / library ACL in Admin → Users.
OPDS Basic auth needs a **local** password account — with Plex-only auth, use a session cookie in the client
or keep a local admin (`AUTH_PROVIDER=local`) for OPDS.


`go test ./...` covers filename parsing, natural sort, merging, age rating, path confinement and archive reading.
