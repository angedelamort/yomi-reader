package library

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"yomi-reader/auth"
)

// Store wraps the SQLite database with typed queries.
type Store struct {
	db *sql.DB
}

// NewStore creates a Store over an open database.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// DB exposes the underlying handle (used by the scanner).
func (s *Store) DB() *sql.DB { return s.db }

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

// GetSetting returns a setting value or def when unset.
func (s *Store) GetSetting(key, def string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return def, nil
	}
	if err != nil {
		return def, err
	}
	return v, nil
}

// SetSetting upserts a setting.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// ---------------------------------------------------------------------------
// Libraries
// ---------------------------------------------------------------------------

const libraryCols = `l.id, l.name, l.root_path, l.created_at, l.last_scan,
	(SELECT COUNT(*) FROM series s WHERE s.library_id = l.id)`

func scanLibrary(sc interface{ Scan(...any) error }) (*Library, error) {
	var l Library
	var created, last int64
	if err := sc.Scan(&l.ID, &l.Name, &l.RootPath, &created, &last, &l.SeriesCount); err != nil {
		return nil, err
	}
	l.CreatedAt = unixTime(created)
	l.LastScan = unixTimePtr(last)
	return &l, nil
}

// ListLibraries returns every library.
func (s *Store) ListLibraries() ([]*Library, error) {
	rows, err := s.db.Query(`SELECT ` + libraryCols + ` FROM libraries l ORDER BY l.name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Library{}
	for rows.Next() {
		l, err := scanLibrary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ListLibrariesForUser returns the libraries the viewer may access.
func (s *Store) ListLibrariesForUser(v Viewer) ([]*Library, error) {
	if v.IsAdmin {
		return s.ListLibraries()
	}
	rows, err := s.db.Query(`SELECT `+libraryCols+` FROM libraries l
		WHERE l.id IN (SELECT library_id FROM library_access WHERE username = ?)
		ORDER BY l.name COLLATE NOCASE`, v.Username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Library{}
	for rows.Next() {
		l, err := scanLibrary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// GetLibrary returns one library or ErrNotFound.
func (s *Store) GetLibrary(id string) (*Library, error) {
	l, err := scanLibrary(s.db.QueryRow(`SELECT `+libraryCols+` FROM libraries l WHERE l.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return l, err
}

// CreateLibrary inserts a library.
func (s *Store) CreateLibrary(name, root string) (*Library, error) {
	id := newID()
	if _, err := s.db.Exec(`INSERT INTO libraries(id, name, root_path, created_at) VALUES(?,?,?,?)`,
		id, name, root, nowUnix()); err != nil {
		return nil, err
	}
	return s.GetLibrary(id)
}

// UpdateLibrary renames a library and/or changes its root path.
func (s *Store) UpdateLibrary(id, name, root string) error {
	res, err := s.db.Exec(`UPDATE libraries SET name = ?, root_path = ? WHERE id = ?`, name, root, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteLibrary removes a library (cascading to its catalog) and returns the
// ids of the series that were removed, so callers can drop cached covers.
func (s *Store) DeleteLibrary(id string) ([]string, error) {
	ids, err := s.seriesIDs(id)
	if err != nil {
		return nil, err
	}
	res, err := s.db.Exec(`DELETE FROM libraries WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return ids, nil
}

func (s *Store) seriesIDs(libraryID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT id FROM series WHERE library_id = ?`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) touchLibraryScan(id string) error {
	_, err := s.db.Exec(`UPDATE libraries SET last_scan = ? WHERE id = ?`, nowUnix(), id)
	return err
}

// CanAccessLibrary reports whether the viewer may use the library at all.
func (s *Store) CanAccessLibrary(v Viewer, libraryID string) (bool, error) {
	if v.IsAdmin {
		return true, nil
	}
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM library_access WHERE library_id = ? AND username = ?`,
		libraryID, v.Username).Scan(&n)
	return n > 0, err
}

// ---------------------------------------------------------------------------
// Users
// ---------------------------------------------------------------------------

func (s *Store) userLibraries(username string) ([]string, error) {
	rows, err := s.db.Query(`SELECT library_id FROM library_access WHERE username = ? ORDER BY library_id`, username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func scanUser(sc interface{ Scan(...any) error }) (*User, error) {
	var u User
	var admin int
	var created, seen int64
	if err := sc.Scan(&u.Username, &u.Email, &u.PasswordHash, &admin, &u.MaxAgeRating, &created, &seen); err != nil {
		return nil, err
	}
	u.IsAdmin = admin != 0
	u.HasPassword = u.PasswordHash != ""
	u.CreatedAt = unixTime(created)
	u.LastSeen = unixTimePtr(seen)
	u.Libraries = []string{}
	return &u, nil
}

const userCols = `username, email, password_hash, is_admin, max_age_rating, created_at, last_seen`

// GetUser returns a user with their library whitelist, or ErrNotFound.
func (s *Store) GetUser(username string) (*User, error) {
	u, err := scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE username = ?`, username))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if u.Libraries, err = s.userLibraries(u.Username); err != nil {
		return nil, err
	}
	return u, nil
}

// ListUsers returns all users with their library whitelists.
func (s *Store) ListUsers() ([]*User, error) {
	rows, err := s.db.Query(`SELECT ` + userCols + ` FROM users ORDER BY username COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	var users []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		users = append(users, u)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, u := range users {
		if u.Libraries, err = s.userLibraries(u.Username); err != nil {
			return nil, err
		}
	}
	if users == nil {
		users = []*User{}
	}
	return users, nil
}

// ViewerFor builds the Viewer for a username from the database (fresh admin
// flag and age limit).
func (s *Store) ViewerFor(username string) (Viewer, error) {
	var admin int
	var rating string
	err := s.db.QueryRow(`SELECT username, is_admin, max_age_rating FROM users WHERE username = ?`, username).
		Scan(&username, &admin, &rating)
	if errors.Is(err, sql.ErrNoRows) {
		return Viewer{}, ErrNotFound
	}
	if err != nil {
		return Viewer{}, err
	}
	if normalizeRating(rating) == "" {
		rating = RatingEveryone
	}
	return Viewer{Username: username, IsAdmin: admin != 0, MaxRating: normalizeRating(rating)}, nil
}

// CountUsers returns the number of accounts.
func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// CountAdmins returns the number of admin accounts.
func (s *Store) CountAdmins() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE is_admin = 1`).Scan(&n)
	return n, err
}

// CreateUser inserts a user. hash may be empty (mock / passwordless).
func (s *Store) CreateUser(u *User, hash string) error {
	rating := normalizeRating(u.MaxAgeRating)
	if rating == "" {
		rating = RatingEveryone
	}
	_, err := s.db.Exec(`INSERT INTO users(username, email, password_hash, is_admin, max_age_rating, created_at)
		VALUES(?,?,?,?,?,?)`, u.Username, u.Email, hash, boolInt(u.IsAdmin), rating, nowUnix())
	return err
}

// UserPatch carries optional user changes; nil fields are left untouched.
type UserPatch struct {
	Email        *string
	IsAdmin      *bool
	MaxAgeRating *string
	PasswordHash *string
}

// UpdateUser applies a partial update.
func (s *Store) UpdateUser(username string, p UserPatch) error {
	var sets []string
	var args []any
	if p.Email != nil {
		sets = append(sets, "email = ?")
		args = append(args, *p.Email)
	}
	if p.IsAdmin != nil {
		sets = append(sets, "is_admin = ?")
		args = append(args, boolInt(*p.IsAdmin))
	}
	if p.MaxAgeRating != nil {
		sets = append(sets, "max_age_rating = ?")
		args = append(args, *p.MaxAgeRating)
	}
	if p.PasswordHash != nil {
		sets = append(sets, "password_hash = ?")
		args = append(args, *p.PasswordHash)
	}
	if len(sets) == 0 {
		_, err := s.GetUser(username)
		return err
	}
	args = append(args, username)
	res, err := s.db.Exec(`UPDATE users SET `+strings.Join(sets, ", ")+` WHERE username = ?`, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteUser removes a user and, by cascade, their progress/favorites/access.
func (s *Store) DeleteUser(username string) error {
	res, err := s.db.Exec(`DELETE FROM users WHERE username = ?`, username)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetUserLibraries replaces a user's library whitelist.
func (s *Store) SetUserLibraries(username string, libraryIDs []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM library_access WHERE username = ?`, username); err != nil {
		return err
	}
	for _, id := range libraryIDs {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO library_access(library_id, username) VALUES(?, ?)`, id, username); err != nil {
			return fmt.Errorf("grant library %q: %w", id, err)
		}
	}
	return tx.Commit()
}

// UpsertExternalUser records a login from a passwordless provider (mock),
// creating the user on first sight and refreshing email/admin flag after.
func (s *Store) UpsertExternalUser(username, email string, isAdmin bool) error {
	rating := RatingTeen
	if isAdmin {
		rating = RatingExplicit
	}
	_, err := s.db.Exec(`INSERT INTO users(username, email, is_admin, max_age_rating, created_at, last_seen)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(username) DO UPDATE SET email = excluded.email, is_admin = excluded.is_admin, last_seen = excluded.last_seen`,
		username, email, boolInt(isAdmin), rating, nowUnix(), nowUnix())
	return err
}

// TouchLastSeen records a login time.
func (s *Store) TouchLastSeen(username string) {
	_, _ = s.db.Exec(`UPDATE users SET last_seen = ? WHERE username = ?`, nowUnix(), username)
}

// LookupCredentials implements auth.CredentialStore.
func (s *Store) LookupCredentials(username string) (*auth.User, string, error) {
	var name, email, hash string
	var admin int
	err := s.db.QueryRow(`SELECT username, email, password_hash, is_admin FROM users WHERE username = ?`, username).
		Scan(&name, &email, &hash, &admin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", auth.ErrUserNotFound
	}
	if err != nil {
		return nil, "", err
	}
	return &auth.User{Username: name, Email: email, IsAdmin: admin != 0}, hash, nil
}

// UserCheck implements auth.UserCheck: returns the live admin flag.
func (s *Store) UserCheck(username string) (bool, bool) {
	var admin int
	err := s.db.QueryRow(`SELECT is_admin FROM users WHERE username = ?`, username).Scan(&admin)
	if err != nil {
		return false, false
	}
	return admin != 0, true
}

// GetPrefs returns the raw preferences JSON for a user ("{}" when unset).
func (s *Store) GetPrefs(username string) (string, error) {
	var p string
	err := s.db.QueryRow(`SELECT prefs FROM user_preferences WHERE username = ?`, username).Scan(&p)
	if errors.Is(err, sql.ErrNoRows) {
		return "{}", nil
	}
	return p, err
}

// SetPrefs stores the preferences JSON for a user.
func (s *Store) SetPrefs(username, prefs string) error {
	_, err := s.db.Exec(`INSERT INTO user_preferences(username, prefs) VALUES(?, ?)
		ON CONFLICT(username) DO UPDATE SET prefs = excluded.prefs`, username, prefs)
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------
// Series
// ---------------------------------------------------------------------------

// seriesCols: the first placeholder is the username used for the favorite flag.
const seriesCols = `s.id, s.library_id, s.title, s.sort_title, s.normalized_title, s.summary, s.authors,
	s.status, s.year, s.age_rating, s.age_locked, s.genres, s.tags, s.cover_rel, s.cover_locked, s.meta_locked, s.chapter_count,
	s.added_at, s.updated_at,
	(SELECT group_concat(n, char(31)) FROM (
		SELECT so.name AS n FROM series_locals sl JOIN sources so ON so.id = sl.source_id
		WHERE sl.series_id = s.id GROUP BY so.name ORDER BY MIN(so.priority), so.name)),
	EXISTS(SELECT 1 FROM favorites f WHERE f.username = ? AND f.series_id = s.id)`

func scanSeries(sc interface{ Scan(...any) error }, extra ...any) (*Series, error) {
	var se Series
	var locked, coverLocked, metaLocked, fav int
	var genres, tags string
	var added, updated int64
	var srcs sql.NullString
	dest := []any{&se.ID, &se.LibraryID, &se.Title, &se.SortTitle, &se.NormalizedTitle, &se.Summary, &se.Authors,
		&se.Status, &se.Year, &se.AgeRating, &locked, &genres, &tags, &se.CoverRel, &coverLocked, &metaLocked, &se.ChapterCount,
		&added, &updated, &srcs, &fav}
	dest = append(dest, extra...)
	if err := sc.Scan(dest...); err != nil {
		return nil, err
	}
	se.AgeLocked = locked != 0
	se.CoverLocked = coverLocked != 0
	se.MetaLocked = metaLocked != 0
	se.Genres = splitCSV(genres)
	se.Tags = splitCSV(tags)
	se.HasCover = se.CoverRel != ""
	se.AddedAt = unixTime(added)
	se.UpdatedAt = unixTime(updated)
	se.Favorite = fav != 0
	se.Sources = []string{}
	if srcs.Valid && srcs.String != "" {
		se.Sources = strings.Split(srcs.String, "\x1f")
	}
	se.CoverURL = "/api/covers/" + se.ID
	return &se, nil
}

// accessClause builds the SQL fragment restricting series (alias a) to the
// viewer's library whitelist and age limit.
//
// Series without any readable chapter (still being scanned, or only holding
// unsupported files) are hidden from every catalog query.
func accessClause(v Viewer, a string) (string, []any) {
	if v.IsAdmin {
		return fmt.Sprintf("%s.chapter_count > 0", a), nil
	}
	return fmt.Sprintf(`(%[1]s.chapter_count > 0
		AND %[1]s.library_id IN (SELECT library_id FROM library_access WHERE username = ?)
		AND CASE %[1]s.age_rating WHEN 'E' THEN 0 WHEN 'T' THEN 1 WHEN 'M' THEN 2 ELSE 3 END <= ?)`, a),
		[]any{v.Username, ratingRank(v.MaxRating)}
}

// seriesScope combines accessClause with an optional catalog age mode.
// ageMode: "" (no extra filter), "safe" (E/T/M only), "adult" (X only).
func seriesScope(v Viewer, a, ageMode string) (string, []any, error) {
	ac, args := accessClause(v, a)
	rank := `CASE %s.age_rating WHEN 'E' THEN 0 WHEN 'T' THEN 1 WHEN 'M' THEN 2 ELSE 3 END`
	switch strings.ToLower(strings.TrimSpace(ageMode)) {
	case "", "all":
		return ac, args, nil
	case "safe":
		// Home catalog: Mature and below — never X.
		return ac + fmt.Sprintf(" AND "+rank+" <= ?", a), append(append([]any{}, args...), ratingRank(RatingMature)), nil
	case "adult", "x":
		if !v.CanSeeRating(RatingExplicit) {
			return "", nil, ErrForbidden
		}
		// Adult catalog: Explicit only.
		return ac + fmt.Sprintf(" AND %s.age_rating = ?", a), append(append([]any{}, args...), RatingExplicit), nil
	default:
		return "", nil, fmt.Errorf("invalid ageMode")
	}
}

// SeriesQuery filters and sorts the catalog listing.
type SeriesQuery struct {
	Q             string
	Tags          []string
	Source        string
	Age           string // optional upper bound on age rating
	AgeMode       string // "", "safe", or "adult"
	Sort          string
	Order         string
	Offset        int
	Limit         int
	LibraryID     string
	OnlyFavorites bool
}

var sortColumns = map[string]struct {
	expr string
	desc bool
}{
	"title":    {"s.sort_title COLLATE NOCASE", false},
	"added":    {"s.added_at", true},
	"updated":  {"s.updated_at", true},
	"chapters": {"s.chapter_count", true},
	"year":     {"s.year", true},
	"random":   {"RANDOM()", false},
}

// ListSeries returns a filtered, sorted, paginated page of series visible to v.
func (s *Store) ListSeries(v Viewer, q SeriesQuery) (*SeriesPage, error) {
	ac, acArgs, err := seriesScope(v, "s", q.AgeMode)
	if err != nil {
		return nil, err
	}
	where := []string{ac}
	args := append([]any{}, acArgs...)

	if q.LibraryID != "" {
		where = append(where, "s.library_id = ?")
		args = append(args, q.LibraryID)
	}
	for _, tok := range strings.Fields(q.Q) {
		like := "%" + escapeLike(tok) + "%"
		where = append(where, `(s.title LIKE ? ESCAPE '\' OR s.normalized_title LIKE ? ESCAPE '\' OR s.authors LIKE ? ESCAPE '\'
			OR s.genres LIKE ? ESCAPE '\' OR s.tags LIKE ? ESCAPE '\'
			OR EXISTS (SELECT 1 FROM series_locals sl WHERE sl.series_id = s.id AND sl.local_title LIKE ? ESCAPE '\'))`)
		args = append(args, like, "%"+escapeLike(strings.ToLower(tok))+"%", like, like, like, like)
	}
	for _, t := range q.Tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		where = append(where, `(',' || s.genres || ',' || s.tags || ',') LIKE ? ESCAPE '\'`)
		args = append(args, "%,"+escapeLike(t)+",%")
	}
	if q.Source != "" {
		where = append(where, `EXISTS (SELECT 1 FROM series_locals sl JOIN sources so ON so.id = sl.source_id
			WHERE sl.series_id = s.id AND (so.id = ? OR so.name = ? COLLATE NOCASE))`)
		args = append(args, q.Source, q.Source)
	}
	if r := normalizeRating(q.Age); r != "" {
		where = append(where, `CASE s.age_rating WHEN 'E' THEN 0 WHEN 'T' THEN 1 WHEN 'M' THEN 2 ELSE 3 END <= ?`)
		args = append(args, ratingRank(r))
	}
	if q.OnlyFavorites {
		where = append(where, `EXISTS (SELECT 1 FROM favorites f2 WHERE f2.username = ? AND f2.series_id = s.id)`)
		args = append(args, v.Username)
	}
	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM series s WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, err
	}

	col, ok := sortColumns[strings.ToLower(q.Sort)]
	if !ok {
		col = sortColumns["title"]
	}
	desc := col.desc
	switch strings.ToLower(q.Order) {
	case "asc":
		desc = false
	case "desc":
		desc = true
	}
	dir := "ASC"
	if desc {
		dir = "DESC"
	}
	order := col.expr + " " + dir
	if col.expr != "RANDOM()" {
		order += ", s.sort_title COLLATE NOCASE ASC"
	}

	limit := clampInt(q.Limit, 1, 200)
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}

	full := append([]any{v.Username}, args...)
	full = append(full, limit, offset)
	rows, err := s.db.Query(`SELECT `+seriesCols+` FROM series s WHERE `+whereSQL+
		` ORDER BY `+order+` LIMIT ? OFFSET ?`, full...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []*Series{}
	for rows.Next() {
		se, err := scanSeries(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, se)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &SeriesPage{Items: items, Total: total, Offset: offset, Limit: limit}, nil
}

// GetSeries returns a series if the viewer may see it, else ErrNotFound.
func (s *Store) GetSeries(v Viewer, id string) (*Series, error) {
	ac, acArgs := accessClause(v, "s")
	args := append([]any{v.Username, id}, acArgs...)
	se, err := scanSeries(s.db.QueryRow(`SELECT `+seriesCols+` FROM series s WHERE s.id = ? AND `+ac, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return se, err
}

// querySeriesList runs a "SELECT seriesCols ..." style query returning series.
func (s *Store) querySeriesList(query string, args ...any) ([]*Series, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Series{}
	for rows.Next() {
		se, err := scanSeries(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, se)
	}
	return out, rows.Err()
}

// ListFavorites returns the user's favorite series, newest first.
func (s *Store) ListFavorites(v Viewer, limit int, ageMode string) ([]*Series, error) {
	ac, acArgs, err := seriesScope(v, "s", ageMode)
	if err != nil {
		return nil, err
	}
	args := append([]any{v.Username, v.Username}, acArgs...)
	args = append(args, limit)
	return s.querySeriesList(`SELECT `+seriesCols+` FROM favorites fv JOIN series s ON s.id = fv.series_id
		WHERE fv.username = ? AND `+ac+` ORDER BY fv.created_at DESC LIMIT ?`, args...)
}

// ListNewest returns series ordered by added_at (kind "added") or updated_at ("updated").
func (s *Store) ListNewest(v Viewer, kind string, limit int, ageMode string) ([]*Series, error) {
	ac, acArgs, err := seriesScope(v, "s", ageMode)
	if err != nil {
		return nil, err
	}
	col := "s.added_at"
	if kind == "updated" {
		col = "s.updated_at"
	}
	args := append([]any{v.Username}, acArgs...)
	args = append(args, limit)
	return s.querySeriesList(`SELECT `+seriesCols+` FROM series s WHERE `+ac+
		` ORDER BY `+col+` DESC, s.sort_title COLLATE NOCASE LIMIT ?`, args...)
}

// ListRandom returns a sample of series visible to v, shuffled deterministically
// by seed. The same seed yields the same selection, so callers can hold a home
// swimlane stable across refreshes via TimeBucketSeed.
func (s *Store) ListRandom(v Viewer, limit int, seed int64, ageMode string) ([]*Series, error) {
	ac, acArgs, err := seriesScope(v, "s", ageMode)
	if err != nil {
		return nil, err
	}
	const prime = 2147483647
	mult := (seed%prime+prime)%prime*2654435761 + 1
	args := append([]any{v.Username}, acArgs...)
	args = append(args, mult, limit)
	return s.querySeriesList(`SELECT `+seriesCols+` FROM series s WHERE `+ac+
		` ORDER BY ((s.rowid + 1) * ?) % `+fmt.Sprintf("%d", prime)+` LIMIT ?`, args...)
}

// TimeBucketSeed returns a seed that stays constant within each window-long
// interval of wall-clock time and changes when the interval rolls over.
func TimeBucketSeed(window time.Duration) int64 {
	if window <= 0 {
		return time.Now().UnixNano()
	}
	return time.Now().UnixNano() / int64(window)
}

// ListTags aggregates genres and tags across the series visible to v.
func (s *Store) ListTags(v Viewer, ageMode string) ([]TagCount, error) {
	ac, acArgs, err := seriesScope(v, "s", ageMode)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT s.genres, s.tags FROM series s WHERE `+ac, acArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]*TagCount{}
	for rows.Next() {
		var g, t string
		if err := rows.Scan(&g, &t); err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, name := range append(splitCSV(g), splitCSV(t)...) {
			k := strings.ToLower(name)
			if seen[k] {
				continue
			}
			seen[k] = true
			if c, ok := counts[k]; ok {
				c.Count++
			} else {
				counts[k] = &TagCount{Name: name, Count: 1}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]TagCount, 0, len(counts))
	for _, c := range counts {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// ListSources returns the sources having at least one series visible to v.
func (s *Store) ListSources(v Viewer, ageMode string) ([]*Source, error) {
	ac, acArgs, err := seriesScope(v, "s", ageMode)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT so.id, so.library_id, so.name, so.priority, so.fs_path, COUNT(DISTINCT s.id)
		FROM sources so JOIN series_locals sl ON sl.source_id = so.id JOIN series s ON s.id = sl.series_id
		WHERE `+ac+` GROUP BY so.id ORDER BY so.priority, so.name COLLATE NOCASE`, acArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSources(rows)
}

// ListAllSources returns every source (admin view), including empty ones.
func (s *Store) ListAllSources() ([]*Source, error) {
	rows, err := s.db.Query(`SELECT so.id, so.library_id, so.name, so.priority, so.fs_path,
		(SELECT COUNT(DISTINCT sl.series_id) FROM series_locals sl WHERE sl.source_id = so.id)
		FROM sources so ORDER BY so.priority, so.name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSources(rows)
}

func scanSources(rows *sql.Rows) ([]*Source, error) {
	out := []*Source{}
	for rows.Next() {
		var so Source
		if err := rows.Scan(&so.ID, &so.LibraryID, &so.Name, &so.Priority, &so.FSPath, &so.SeriesCount); err != nil {
			return nil, err
		}
		out = append(out, &so)
	}
	return out, rows.Err()
}

// SetSeriesAgeRating overrides a series' age rating and locks it against rescans.
func (s *Store) SetSeriesAgeRating(id, rating string, locked bool) error {
	res, err := s.db.Exec(`UPDATE series SET age_rating = ?, age_locked = ? WHERE id = ?`, rating, boolInt(locked), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SeriesMetaPatch is a partial admin update for series display metadata.
// Nil pointer fields are left unchanged. Empty slices clear genres/tags.
type SeriesMetaPatch struct {
	Title      *string
	Summary    *string
	Authors    *string
	Status     *string
	Year       *int
	Genres     *[]string
	Tags       *[]string
	AgeRating  *string // "auto" unlocks; E/T/M/X sets and locks
	MetaLocked *bool   // nil = lock when any text field is written
}

// UpdateSeriesMeta applies an admin metadata patch. Writing any text field
// locks metadata against ComicInfo fills on rescan unless MetaLocked is set.
func (s *Store) UpdateSeriesMeta(id string, p SeriesMetaPatch) error {
	var title, summary, authors, status, genres, tags, age string
	var year, ageLocked, metaLocked int
	err := s.db.QueryRow(`SELECT title, summary, authors, status, year, genres, tags, age_rating, age_locked, meta_locked
		FROM series WHERE id = ?`, id).
		Scan(&title, &summary, &authors, &status, &year, &genres, &tags, &age, &ageLocked, &metaLocked)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	touchedMeta := false
	if p.Title != nil {
		title = strings.TrimSpace(*p.Title)
		if title == "" {
			return fmt.Errorf("%w: title cannot be empty", ErrBadRequest)
		}
		touchedMeta = true
	}
	if p.Summary != nil {
		summary = strings.TrimSpace(*p.Summary)
		touchedMeta = true
	}
	if p.Authors != nil {
		authors = strings.TrimSpace(*p.Authors)
		touchedMeta = true
	}
	if p.Status != nil {
		status = strings.TrimSpace(*p.Status)
		touchedMeta = true
	}
	if p.Year != nil {
		year = *p.Year
		if year < 0 || year > 9999 {
			return fmt.Errorf("%w: year out of range", ErrBadRequest)
		}
		touchedMeta = true
	}
	if p.Genres != nil {
		genres = joinCSV(*p.Genres)
		touchedMeta = true
	}
	if p.Tags != nil {
		tags = joinCSV(*p.Tags)
		touchedMeta = true
	}
	if p.MetaLocked != nil {
		metaLocked = boolInt(*p.MetaLocked)
	} else if touchedMeta {
		metaLocked = 1
	}

	if p.AgeRating != nil {
		v := strings.TrimSpace(*p.AgeRating)
		if strings.EqualFold(v, "auto") {
			ageLocked = 0
		} else {
			r := normalizeRating(v)
			if r == "" {
				return fmt.Errorf("%w: ageRating must be E, T, M, X or auto", ErrBadRequest)
			}
			age = r
			ageLocked = 1
		}
	}

	res, err := s.db.Exec(`UPDATE series SET title = ?, sort_title = ?, summary = ?, authors = ?, status = ?, year = ?,
		genres = ?, tags = ?, age_rating = ?, age_locked = ?, meta_locked = ?, updated_at = ? WHERE id = ?`,
		title, sortTitle(title), summary, authors, status, year, genres, tags, age, ageLocked, metaLocked, nowUnix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) setSeriesCover(id, rel string) {
	// Bump updated_at so clients can cache-bust /api/covers/{id} after overrides.
	_, _ = s.db.Exec(`UPDATE series SET cover_rel = ?, updated_at = ? WHERE id = ?`, rel, nowUnix(), id)
}

// SetCoverLocked marks a series cover as manually overridden (or unlocks it).
func (s *Store) SetCoverLocked(id string, locked bool) error {
	res, err := s.db.Exec(`UPDATE series SET cover_locked = ?, updated_at = ? WHERE id = ?`, boolInt(locked), nowUnix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// CoverLocked reports whether the series cover is manually overridden.
func (s *Store) CoverLocked(id string) (bool, error) {
	var locked int
	err := s.db.QueryRow(`SELECT cover_locked FROM series WHERE id = ?`, id).Scan(&locked)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	return locked != 0, nil
}

// ---------------------------------------------------------------------------
// Chapters
// ---------------------------------------------------------------------------

const chapterCols = `c.id, c.series_id, c.source_id, so.name, so.priority, c.number, c.volume, c.title,
	c.fs_path, c.format, c.page_count, c.size_bytes, c.mtime, c.released_at, c.scanned_at`

func scanChapter(sc interface{ Scan(...any) error }, extra ...any) (*Chapter, error) {
	var c Chapter
	var vol sql.NullFloat64
	var released, scanned int64
	dest := []any{&c.ID, &c.SeriesID, &c.SourceID, &c.SourceName, &c.Priority, &c.Number, &vol, &c.Title,
		&c.FSPath, &c.Format, &c.PageCount, &c.SizeBytes, &c.MTime, &released, &scanned}
	dest = append(dest, extra...)
	if err := sc.Scan(dest...); err != nil {
		return nil, err
	}
	if vol.Valid {
		v := vol.Float64
		c.Volume = &v
	}
	c.ReleasedAt = unixTimePtr(released)
	c.ScannedAt = unixTime(scanned)
	return &c, nil
}

// ListChapters returns every stored chapter of a series (all sources), ordered
// by number then source priority.
func (s *Store) ListChapters(seriesID string) ([]*Chapter, error) {
	rows, err := s.db.Query(`SELECT `+chapterCols+` FROM chapters c JOIN sources so ON so.id = c.source_id
		WHERE c.series_id = ? ORDER BY c.number, so.priority, c.fs_path`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Chapter{}
	for rows.Next() {
		c, err := scanChapter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

const chapterRefSelect = `SELECT ` + chapterCols + `, s.library_id, l.root_path, s.age_rating
	FROM chapters c JOIN sources so ON so.id = c.source_id
	JOIN series s ON s.id = c.series_id JOIN libraries l ON l.id = s.library_id `

// GetChapterRef returns a chapter with library context, or ErrNotFound. It does
// not apply access checks; callers must verify the viewer.
func (s *Store) GetChapterRef(id string) (*ChapterRef, error) {
	var ref ChapterRef
	row := s.db.QueryRow(chapterRefSelect+`WHERE c.id = ?`, id)
	c, err := scanChapter(row, &ref.LibraryID, &ref.LibraryRoot, &ref.SeriesAgeRating)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	ref.Chapter = *c
	return &ref, nil
}

// CoverChapter picks the chapter whose first page becomes the series cover:
// the lowest numbered chapter, preferring the best source.
func (s *Store) CoverChapter(seriesID string) (*ChapterRef, error) {
	var ref ChapterRef
	row := s.db.QueryRow(chapterRefSelect+`WHERE c.series_id = ?
		ORDER BY CASE WHEN c.number >= 0 THEN 0 ELSE 1 END, c.number, so.priority, c.fs_path LIMIT 1`, seriesID)
	c, err := scanChapter(row, &ref.LibraryID, &ref.LibraryRoot, &ref.SeriesAgeRating)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	ref.Chapter = *c
	return &ref, nil
}

// CanViewChapter checks library whitelist and age limit for a chapter.
func (s *Store) CanViewChapter(v Viewer, ref *ChapterRef) (bool, error) {
	if v.IsAdmin {
		return true, nil
	}
	if !v.CanSeeRating(ref.SeriesAgeRating) {
		return false, nil
	}
	return s.CanAccessLibrary(v, ref.LibraryID)
}

// ---------------------------------------------------------------------------
// Progress and favorites
// ---------------------------------------------------------------------------

// GetProgress returns a user's progress for a series, or (nil, nil) if none.
func (s *Store) GetProgress(username, seriesID string) (*Progress, error) {
	var p Progress
	var completed int
	var updated int64
	err := s.db.QueryRow(`SELECT series_id, chapter_id, chapter_number, page_index, scroll_ratio, completed, updated_at
		FROM reading_progress WHERE username = ? AND series_id = ?`, username, seriesID).
		Scan(&p.SeriesID, &p.ChapterID, &p.ChapterNumber, &p.PageIndex, &p.ScrollRatio, &completed, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.Completed = completed != 0
	p.UpdatedAt = unixTime(updated)
	return &p, nil
}

// PutProgress upserts a user's progress for a series.
func (s *Store) PutProgress(username string, p *Progress) error {
	_, err := s.db.Exec(`INSERT INTO reading_progress
		(username, series_id, chapter_id, chapter_number, page_index, scroll_ratio, completed, updated_at)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(username, series_id) DO UPDATE SET
		  chapter_id = excluded.chapter_id, chapter_number = excluded.chapter_number,
		  page_index = excluded.page_index, scroll_ratio = excluded.scroll_ratio,
		  completed = excluded.completed, updated_at = excluded.updated_at`,
		username, p.SeriesID, p.ChapterID, p.ChapterNumber, p.PageIndex, p.ScrollRatio, boolInt(p.Completed), nowUnix())
	return err
}

// ListProgress returns the user's progress rows for accessible series, newest first.
func (s *Store) ListProgress(v Viewer, limit int, ageMode string) ([]*Progress, error) {
	ac, acArgs, err := seriesScope(v, "s", ageMode)
	if err != nil {
		return nil, err
	}
	args := append([]any{v.Username}, acArgs...)
	args = append(args, limit)
	rows, err := s.db.Query(`SELECT rp.series_id, rp.chapter_id, rp.chapter_number, rp.page_index, rp.scroll_ratio,
		rp.completed, rp.updated_at FROM reading_progress rp JOIN series s ON s.id = rp.series_id
		WHERE rp.username = ? AND `+ac+` ORDER BY rp.updated_at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Progress{}
	for rows.Next() {
		var p Progress
		var completed int
		var updated int64
		if err := rows.Scan(&p.SeriesID, &p.ChapterID, &p.ChapterNumber, &p.PageIndex, &p.ScrollRatio, &completed, &updated); err != nil {
			return nil, err
		}
		p.Completed = completed != 0
		p.UpdatedAt = unixTime(updated)
		out = append(out, &p)
	}
	return out, rows.Err()
}

// SetFavorite adds or removes a favorite.
func (s *Store) SetFavorite(username, seriesID string, on bool) error {
	if on {
		_, err := s.db.Exec(`INSERT OR IGNORE INTO favorites(username, series_id, created_at) VALUES(?,?,?)`,
			username, seriesID, nowUnix())
		return err
	}
	_, err := s.db.Exec(`DELETE FROM favorites WHERE username = ? AND series_id = ?`, username, seriesID)
	return err
}

// GetSeriesReaderMode returns the user's reader mode for a series, or "" if unset.
func (s *Store) GetSeriesReaderMode(username, seriesID string) (string, error) {
	var mode string
	err := s.db.QueryRow(`SELECT reader_mode FROM series_preferences WHERE username = ? AND series_id = ?`,
		username, seriesID).Scan(&mode)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return mode, err
}

// SetSeriesReaderMode stores the user's reader mode for a series.
func (s *Store) SetSeriesReaderMode(username, seriesID, mode string) error {
	_, err := s.db.Exec(`INSERT INTO series_preferences(username, series_id, reader_mode) VALUES(?,?,?)
		ON CONFLICT(username, series_id) DO UPDATE SET reader_mode = excluded.reader_mode`,
		username, seriesID, mode)
	return err
}

// ---------------------------------------------------------------------------
// Source priority
// ---------------------------------------------------------------------------

const defaultSourcePriority = 1000

// stripLangSuffix removes a trailing "(EN)" style marker: "MangaDex (EN)" -> "mangadex".
func stripLangSuffix(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.LastIndex(s, "("); i > 0 && strings.HasSuffix(s, ")") {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

// matchSourceName reports whether a priority-list entry designates a source.
func matchSourceName(entry, name string) bool {
	e := strings.ToLower(strings.TrimSpace(entry))
	n := strings.ToLower(strings.TrimSpace(name))
	if e == "" {
		return false
	}
	return e == n || e == stripLangSuffix(name) || stripLangSuffix(entry) == n
}

// priorityFor returns the rank of a source name within the CSV list (0 = best),
// or defaultSourcePriority when unlisted.
func priorityFor(list []string, name string) int {
	for i, e := range list {
		if matchSourceName(e, name) {
			return i
		}
	}
	return defaultSourcePriority
}

// SourcePriorityList returns the configured priority list (best first).
func (s *Store) SourcePriorityList() []string {
	v, _ := s.GetSetting(SettingSourcePriority, "")
	return splitCSV(v)
}

// ApplySourcePriority stores the CSV and rewrites every source's priority.
func (s *Store) ApplySourcePriority(csv string) error {
	list := splitCSV(csv)
	if err := s.SetSetting(SettingSourcePriority, strings.Join(list, ",")); err != nil {
		return err
	}
	sources, err := s.ListAllSources()
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, so := range sources {
		if _, err := tx.Exec(`UPDATE sources SET priority = ? WHERE id = ?`, priorityFor(list, so.Name), so.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---------------------------------------------------------------------------
// Jobs
// ---------------------------------------------------------------------------

func (s *Store) insertJob(j *Job) error {
	_, err := s.db.Exec(`INSERT INTO jobs(id, kind, library_id, label, status, message, total, done, started_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, j.ID, j.Kind, j.LibraryID, j.Label, j.Status, j.Message, j.Total, j.Done, nowUnix())
	return err
}

func (s *Store) updateJob(id, status, message string, total, done int, finished bool) {
	var fin int64
	if finished {
		fin = nowUnix()
	}
	_, _ = s.db.Exec(`UPDATE jobs SET status = ?, message = ?, total = ?, done = ?,
		finished_at = CASE WHEN ? > 0 THEN ? ELSE finished_at END WHERE id = ?`,
		status, message, total, done, fin, fin, id)
}

// ListJobs returns the most recent jobs.
func (s *Store) ListJobs(limit int) ([]*Job, error) {
	rows, err := s.db.Query(`SELECT id, kind, library_id, label, status, message, total, done, started_at, finished_at
		FROM jobs ORDER BY started_at DESC, rowid DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Job{}
	for rows.Next() {
		var j Job
		var started, finished int64
		if err := rows.Scan(&j.ID, &j.Kind, &j.LibraryID, &j.Label, &j.Status, &j.Message, &j.Total, &j.Done, &started, &finished); err != nil {
			return nil, err
		}
		j.StartedAt = unixTime(started)
		j.FinishedAt = unixTimePtr(finished)
		out = append(out, &j)
	}
	return out, rows.Err()
}

// MarkInterruptedJobs flags jobs left "running" by a previous process.
func (s *Store) MarkInterruptedJobs() {
	_, _ = s.db.Exec(`UPDATE jobs SET status = 'interrupted', finished_at = ? WHERE status = 'running'`, nowUnix())
}
