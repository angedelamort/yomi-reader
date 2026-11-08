# TODO

Planned work beyond the current README feature set.

## New chapter alerts

Autoscan already updates the catalog; there is still no user-facing signal.

- Badge / “Just dropped” shelf on Home when chapters appear after the user’s last visit
- Optional outbound hooks: Discord webhook, generic webhook, later Web Push
- Per-user mute / watchlist (at least: favorites + continue-reading series)

## PWA

Shipped (installable shell, cover/page caches). Follow-ups:

- Optional offline chapter download queue (“save for later”)
- `beforeinstallprompt` / in-app Install affordance
- Push notifications wired to new-chapter alerts above

## External metadata enrichment

ComicInfo + admin edit is not enough for rich series pages. Multiple providers
make this non-trivial:

- Pluggable providers (AniList, MAL, MangaDex, Comic Vine, …)
- Match by title / external IDs, preview before apply, respect `metaLocked` / `coverLocked`
- Admin “Search metadata” on the series page; optional auto-fill on first scan

## Reader pro

- Night / contrast / invert filters
- Stronger mobile gestures, per-series keybinds
- Double-page / spreads (LTR/RTL)
- More aggressive preload; optional panel-aware navigation later

## Personal recommendations (+ Suwayomi / external)

Local-only “because you read X” is weak without outside taste graphs.

- Integration module that can call **Suwayomi** (and later AniList-style sources)
  for related titles / trending / “more like this”
- Surface results on Home, preferring series already in the local library, with
  clear “not in library” affordances when Suwayomi can fetch them
- Keep ACL / age caps when projecting remote recs onto local catalog

## Reading lists

**Favorites** (series-only list) are done — toggle on the series page / Home shelf,
not in the reader. Later: real multi-lists.

- Built-ins: Want to read / Reading / On hold / Dropped / Completed
- Custom user lists; add/remove from series page and Home shelves
- Optional: share a list within the household (same server users)

## Auth

**Plex** is done (`AUTH_PROVIDER=plex`, `PLEX_MACHINE_ID`, popup PIN + `/api/auth/plex/exchange`).

- Later: Google OAuth as an additional provider
