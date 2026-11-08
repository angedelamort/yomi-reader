package library

import (
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"

	_ "modernc.org/sqlite" // CGO-free SQLite driver
)

// schema is applied on every start; all statements are idempotent.
const schema = `
CREATE TABLE IF NOT EXISTS libraries (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  root_path  TEXT NOT NULL,
  created_at INTEGER NOT NULL DEFAULT 0,
  last_scan  INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS users (
  username       TEXT PRIMARY KEY COLLATE NOCASE,
  email          TEXT NOT NULL DEFAULT '',
  password_hash  TEXT NOT NULL DEFAULT '',
  is_admin       INTEGER NOT NULL DEFAULT 0,
  max_age_rating TEXT NOT NULL DEFAULT 'E',
  created_at     INTEGER NOT NULL DEFAULT 0,
  last_seen      INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS library_access (
  library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  username   TEXT NOT NULL COLLATE NOCASE REFERENCES users(username) ON DELETE CASCADE ON UPDATE CASCADE,
  PRIMARY KEY (library_id, username)
);

CREATE TABLE IF NOT EXISTS settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS sources (
  id         TEXT PRIMARY KEY,
  library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  priority   INTEGER NOT NULL DEFAULT 1000,
  fs_path    TEXT NOT NULL,
  UNIQUE (library_id, name)
);

CREATE TABLE IF NOT EXISTS series (
  id               TEXT PRIMARY KEY,
  library_id       TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  title            TEXT NOT NULL,
  sort_title       TEXT NOT NULL DEFAULT '',
  normalized_title TEXT NOT NULL,
  summary          TEXT NOT NULL DEFAULT '',
  authors          TEXT NOT NULL DEFAULT '',
  status           TEXT NOT NULL DEFAULT '',
  year             INTEGER NOT NULL DEFAULT 0,
  age_rating       TEXT NOT NULL DEFAULT 'T',
  age_locked       INTEGER NOT NULL DEFAULT 0,
  genres           TEXT NOT NULL DEFAULT '',
  tags             TEXT NOT NULL DEFAULT '',
  cover_rel        TEXT NOT NULL DEFAULT '',
  cover_locked     INTEGER NOT NULL DEFAULT 0,
  meta_locked      INTEGER NOT NULL DEFAULT 0,
  source_priority  TEXT NOT NULL DEFAULT '', -- CSV of source names, best first; empty = global default
  chapter_count    INTEGER NOT NULL DEFAULT 0,
  added_at         INTEGER NOT NULL DEFAULT 0,
  updated_at       INTEGER NOT NULL DEFAULT 0,
  UNIQUE (library_id, normalized_title)
);
CREATE INDEX IF NOT EXISTS idx_series_library ON series(library_id);
CREATE INDEX IF NOT EXISTS idx_series_sort    ON series(sort_title);
CREATE INDEX IF NOT EXISTS idx_series_added   ON series(added_at);
CREATE INDEX IF NOT EXISTS idx_series_updated ON series(updated_at);

-- Physical folder per source for a merged series.
CREATE TABLE IF NOT EXISTS series_locals (
  id          TEXT PRIMARY KEY,
  series_id   TEXT NOT NULL REFERENCES series(id) ON DELETE CASCADE,
  source_id   TEXT NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
  local_title TEXT NOT NULL,
  fs_path     TEXT NOT NULL UNIQUE,
  cover_rel   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_locals_series ON series_locals(series_id);
CREATE INDEX IF NOT EXISTS idx_locals_source ON series_locals(source_id);

-- number is -1 when it could not be parsed from the filename.
CREATE TABLE IF NOT EXISTS chapters (
  id          TEXT PRIMARY KEY,
  series_id   TEXT NOT NULL REFERENCES series(id) ON DELETE CASCADE,
  source_id   TEXT NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
  number      REAL NOT NULL DEFAULT -1,
  volume      REAL,
  title       TEXT NOT NULL DEFAULT '',
  fs_path     TEXT NOT NULL UNIQUE,
  format      TEXT NOT NULL,
  page_count  INTEGER NOT NULL DEFAULT 0,
  size_bytes  INTEGER NOT NULL DEFAULT 0,
  mtime       INTEGER NOT NULL DEFAULT 0,
  released_at INTEGER NOT NULL DEFAULT 0,
  scanned_at  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_chapters_series ON chapters(series_id, number);
CREATE INDEX IF NOT EXISTS idx_chapters_source ON chapters(source_id);

CREATE TABLE IF NOT EXISTS reading_progress (
  username       TEXT NOT NULL COLLATE NOCASE REFERENCES users(username) ON DELETE CASCADE ON UPDATE CASCADE,
  series_id      TEXT NOT NULL REFERENCES series(id) ON DELETE CASCADE,
  chapter_id     TEXT NOT NULL DEFAULT '',
  chapter_number REAL NOT NULL DEFAULT -1,
  page_index     INTEGER NOT NULL DEFAULT 0,
  scroll_ratio   REAL NOT NULL DEFAULT 0,
  completed      INTEGER NOT NULL DEFAULT 0,
  updated_at     INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (username, series_id)
);
CREATE INDEX IF NOT EXISTS idx_progress_user ON reading_progress(username, updated_at DESC);

CREATE TABLE IF NOT EXISTS favorites (
  username   TEXT NOT NULL COLLATE NOCASE REFERENCES users(username) ON DELETE CASCADE ON UPDATE CASCADE,
  series_id  TEXT NOT NULL REFERENCES series(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (username, series_id)
);

-- Per-user reader mode for a series (ltr | rtl | vertical | webtoon).
CREATE TABLE IF NOT EXISTS series_preferences (
  username    TEXT NOT NULL COLLATE NOCASE REFERENCES users(username) ON DELETE CASCADE ON UPDATE CASCADE,
  series_id   TEXT NOT NULL REFERENCES series(id) ON DELETE CASCADE,
  reader_mode TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (username, series_id)
);

CREATE TABLE IF NOT EXISTS user_preferences (
  username TEXT PRIMARY KEY COLLATE NOCASE REFERENCES users(username) ON DELETE CASCADE ON UPDATE CASCADE,
  prefs    TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS jobs (
  id          TEXT PRIMARY KEY,
  kind        TEXT NOT NULL,
  library_id  TEXT NOT NULL DEFAULT '',
  label       TEXT NOT NULL DEFAULT '',
  status      TEXT NOT NULL,
  message     TEXT NOT NULL DEFAULT '',
  total       INTEGER NOT NULL DEFAULT 0,
  done        INTEGER NOT NULL DEFAULT 0,
  started_at  INTEGER NOT NULL DEFAULT 0,
  finished_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_jobs_started ON jobs(started_at DESC);
`

// OpenDB opens (creating if needed) the SQLite database under dataPath.
func OpenDB(dataPath string) (*sql.DB, error) {
	path := filepath.Join(dataPath, "yomi-reader.db")

	q := url.Values{}
	q.Add("_pragma", "busy_timeout(15000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Add("_pragma", "foreign_keys(1)")
	// Write transactions take the lock up front (avoids SQLITE_BUSY_SNAPSHOT
	// when the concurrent scanner upgrades read transactions).
	q.Add("_txlock", "immediate")
	dsn := "file:" + filepath.ToSlash(path) + "?" + q.Encode()

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(8)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

// migrate adds columns introduced after the initial schema. CREATE TABLE IF NOT
// EXISTS does not alter existing tables, so each new column is applied here.
func migrate(db *sql.DB) error {
	if err := addColumnIfMissing(db, "series", "cover_locked", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "series", "meta_locked", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	return addColumnIfMissing(db, "series", "source_priority", "TEXT NOT NULL DEFAULT ''")
}

func addColumnIfMissing(db *sql.DB, table, column, decl string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + decl)
	return err
}
