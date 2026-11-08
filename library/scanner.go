package library

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrScanRunning is returned when a scan for the library is already in progress.
var ErrScanRunning = errors.New("scan already running for this library")

// maxScanErrors caps the error messages retained per scan.
const maxScanErrors = 100

// ScanStatus is a snapshot of a library scan (running or finished).
type ScanStatus struct {
	LibraryID       string     `json:"libraryId"`
	JobID           string     `json:"jobId"`
	Running         bool       `json:"running"`
	Phase           string     `json:"phase"` // discovering | scanning | cleanup | done | error
	Reason          string     `json:"reason"`
	StartedAt       time.Time  `json:"startedAt"`
	FinishedAt      *time.Time `json:"finishedAt,omitempty"`
	Sources         int        `json:"sources"`
	SeriesTotal     int        `json:"seriesTotal"`
	SeriesDone      int        `json:"seriesDone"`
	SeriesAdded     int        `json:"seriesAdded"`
	SeriesRemoved   int        `json:"seriesRemoved"`
	ChaptersAdded   int        `json:"chaptersAdded"`
	ChaptersUpdated int        `json:"chaptersUpdated"`
	ChaptersRemoved int        `json:"chaptersRemoved"`
	ChaptersSkipped int        `json:"chaptersUnchanged"`
	Unsupported     int        `json:"unsupportedFiles"` // .cbr/.rar/.cb7/.7z: not readable
	ErrorCount      int        `json:"errorCount"`
	Errors          []string   `json:"errors"`
	Error           string     `json:"error,omitempty"`
}

// Scanner indexes library roots into the database. Scans of the same library
// are single-flight; different libraries may scan concurrently.
type Scanner struct {
	store      *Store
	covers     *CoverService
	workers    int
	inspectSem chan struct{} // bounds concurrent archive inspections

	mu   sync.Mutex
	runs map[string]*ScanStatus

	stop     chan struct{}
	stopOnce sync.Once
}

// NewScanner creates a scanner. covers may be nil (covers are then lazy only).
func NewScanner(store *Store, covers *CoverService) *Scanner {
	return &Scanner{
		store:      store,
		covers:     covers,
		workers:    4,
		inspectSem: make(chan struct{}, 24),
		runs:       map[string]*ScanStatus{},
		stop:       make(chan struct{}),
	}
}

// Shutdown asks running scans to stop after the current series.
func (sc *Scanner) Shutdown() { sc.stopOnce.Do(func() { close(sc.stop) }) }

func (sc *Scanner) stopped() bool {
	select {
	case <-sc.stop:
		return true
	default:
		return false
	}
}

// Status returns a copy of the latest scan status for a library (nil if never scanned).
func (sc *Scanner) Status(libraryID string) *ScanStatus {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	st, ok := sc.runs[libraryID]
	if !ok {
		return nil
	}
	cp := *st
	cp.Errors = append([]string{}, st.Errors...)
	return &cp
}

// IsRunning reports whether a scan of the library is in progress.
func (sc *Scanner) IsRunning(libraryID string) bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	st, ok := sc.runs[libraryID]
	return ok && st.Running
}

// ScanAsync starts a background scan; it returns false if one is already running.
func (sc *Scanner) ScanAsync(lib *Library, reason string) bool {
	if sc.IsRunning(lib.ID) {
		return false
	}
	go func() {
		if err := sc.Scan(lib, reason); err != nil && !errors.Is(err, ErrScanRunning) {
			log.Printf("scan %q: %v", lib.Name, err)
		}
	}()
	return true
}

func (sc *Scanner) mutate(st *ScanStatus, fn func(*ScanStatus)) {
	sc.mu.Lock()
	fn(st)
	sc.mu.Unlock()
}

func (sc *Scanner) addError(st *ScanStatus, msg string) {
	sc.mutate(st, func(s *ScanStatus) {
		s.ErrorCount++
		if len(s.Errors) < maxScanErrors {
			s.Errors = append(s.Errors, msg)
		}
	})
}

// Scan indexes a library synchronously.
func (sc *Scanner) Scan(lib *Library, reason string) error {
	sc.mu.Lock()
	if cur, ok := sc.runs[lib.ID]; ok && cur.Running {
		sc.mu.Unlock()
		return ErrScanRunning
	}
	st := &ScanStatus{
		LibraryID: lib.ID,
		JobID:     newID(),
		Running:   true,
		Phase:     "discovering",
		Reason:    reason,
		StartedAt: time.Now().UTC(),
		Errors:    []string{},
	}
	sc.runs[lib.ID] = st
	sc.mu.Unlock()

	_ = sc.store.insertJob(&Job{ID: st.JobID, Kind: "scan", LibraryID: lib.ID,
		Label: fmt.Sprintf("%s (%s)", lib.Name, reason), Status: "running"})
	log.Printf("scan %q: started (%s) root=%s", lib.Name, reason, lib.RootPath)

	err := sc.run(lib, st)

	now := time.Now().UTC()
	sc.mutate(st, func(s *ScanStatus) {
		s.Running = false
		s.FinishedAt = &now
		if err != nil {
			s.Phase = "error"
			s.Error = err.Error()
		} else {
			s.Phase = "done"
		}
	})
	snap := sc.Status(lib.ID)
	status, msg := "done", fmt.Sprintf("+%d/~%d/-%d chapters, +%d/-%d series, %d errors",
		snap.ChaptersAdded, snap.ChaptersUpdated, snap.ChaptersRemoved, snap.SeriesAdded, snap.SeriesRemoved, snap.ErrorCount)
	if err != nil {
		status, msg = "error", err.Error()
	}
	sc.store.updateJob(st.JobID, status, msg, snap.SeriesTotal, snap.SeriesDone, true)
	log.Printf("scan %q: %s — %s", lib.Name, status, msg)
	if err == nil {
		_ = sc.store.touchLibraryScan(lib.ID)
	}
	return err
}

// ---------------------------------------------------------------------------
// Discovery
// ---------------------------------------------------------------------------

type srcRow struct {
	id       string
	name     string
	priority int
}

type scanLocal struct {
	source *srcRow
	name   string
	path   string
}

type scanGroup struct {
	norm   string
	locals []*scanLocal
}

// skipName filters hidden/system entries.
func skipName(name string) bool {
	if name == "" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "~") {
		return true
	}
	switch strings.ToLower(name) {
	case "thumbs.db", "desktop.ini", "@eadir", "$recycle.bin", "system volume information", "lost+found":
		return true
	}
	return false
}

// isDirEntry reports whether the entry is a directory, following symlinks.
func isDirEntry(de os.DirEntry, parent string) bool {
	if de.IsDir() {
		return true
	}
	if de.Type()&os.ModeSymlink != 0 {
		fi, err := os.Stat(filepath.Join(parent, de.Name()))
		return err == nil && fi.IsDir()
	}
	return false
}

// detectMangasRoot returns <root>/mangas when it exists, else root itself.
func detectMangasRoot(root string) string {
	des, err := os.ReadDir(root)
	if err != nil {
		return root
	}
	for _, de := range des {
		if strings.EqualFold(de.Name(), "mangas") && isDirEntry(de, root) {
			return filepath.Join(root, de.Name())
		}
	}
	return root
}

func (sc *Scanner) ensureSource(libraryID, name, fsPath string, prio []string) (*srcRow, error) {
	db := sc.store.db
	var row srcRow
	row.name = name
	err := db.QueryRow(`SELECT id, priority FROM sources WHERE library_id = ? AND name = ?`, libraryID, name).
		Scan(&row.id, &row.priority)
	switch {
	case err == nil:
		_, err = db.Exec(`UPDATE sources SET fs_path = ? WHERE id = ?`, fsPath, row.id)
		return &row, err
	case errors.Is(err, sql.ErrNoRows):
		row.id = newID()
		row.priority = priorityFor(prio, name)
		_, err = db.Exec(`INSERT INTO sources(id, library_id, name, priority, fs_path) VALUES(?,?,?,?,?)`,
			row.id, libraryID, name, row.priority, fsPath)
		return &row, err
	}
	return nil, err
}

func (sc *Scanner) run(lib *Library, st *ScanStatus) error {
	if fi, err := os.Stat(lib.RootPath); err != nil || !fi.IsDir() {
		return fmt.Errorf("library root not accessible: %s", lib.RootPath)
	}
	mangasRoot := detectMangasRoot(lib.RootPath)
	entries, err := os.ReadDir(mangasRoot)
	if err != nil {
		return fmt.Errorf("read %s: %w", mangasRoot, err)
	}

	prio := sc.store.SourcePriorityList()
	safe := true // false => skip destructive cleanup (partial/unreliable listing)
	groups := map[string]*scanGroup{}
	foundSources := map[string]bool{}
	seenLocals := map[string]bool{}

	for _, e := range entries {
		if sc.stopped() {
			return errors.New("scan interrupted")
		}
		if !isDirEntry(e, mangasRoot) || skipName(e.Name()) {
			continue
		}
		spath := filepath.Join(mangasRoot, e.Name())
		src, err := sc.ensureSource(lib.ID, e.Name(), spath, prio)
		if err != nil {
			sc.addError(st, fmt.Sprintf("source %q: %v", e.Name(), err))
			safe = false
			continue
		}
		foundSources[e.Name()] = true
		seriesDirs, err := os.ReadDir(spath)
		if err != nil {
			sc.addError(st, fmt.Sprintf("read source %q: %v", e.Name(), err))
			safe = false
			continue
		}
		for _, se := range seriesDirs {
			if !isDirEntry(se, spath) || skipName(se.Name()) {
				continue
			}
			norm := normalizeTitle(se.Name())
			g := groups[norm]
			if g == nil {
				g = &scanGroup{norm: norm}
				groups[norm] = g
			}
			lp := filepath.Join(spath, se.Name())
			g.locals = append(g.locals, &scanLocal{source: src, name: se.Name(), path: lp})
			seenLocals[lp] = true
		}
	}
	sc.mutate(st, func(s *ScanStatus) {
		s.Sources = len(foundSources)
		s.SeriesTotal = len(groups)
		s.Phase = "scanning"
	})

	// Drop sources that vanished from disk (cascades to their locals/chapters).
	if safe && len(foundSources) > 0 {
		sc.removeMissingSources(lib.ID, foundSources)
	}

	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	work := make(chan *scanGroup)
	var wg sync.WaitGroup
	for i := 0; i < sc.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for g := range work {
				if sc.stopped() {
					continue
				}
				sc.safeProcess(lib, g, st)
				sc.mutate(st, func(s *ScanStatus) { s.SeriesDone++ })
			}
		}()
	}
	for _, k := range keys {
		work <- groups[k]
	}
	close(work)
	wg.Wait()
	if sc.stopped() {
		return errors.New("scan interrupted")
	}

	// Cleanup of series/locals that no longer exist on disk.
	if safe && len(groups) > 0 {
		sc.mutate(st, func(s *ScanStatus) { s.Phase = "cleanup" })
		sc.cleanup(lib.ID, seenLocals, st)
	}
	return nil
}

func (sc *Scanner) removeMissingSources(libraryID string, found map[string]bool) {
	rows, err := sc.store.db.Query(`SELECT id, name FROM sources WHERE library_id = ?`, libraryID)
	if err != nil {
		return
	}
	var gone []string
	for rows.Next() {
		var id, name string
		if rows.Scan(&id, &name) == nil && !found[name] {
			gone = append(gone, id)
		}
	}
	rows.Close()
	for _, id := range gone {
		_, _ = sc.store.db.Exec(`DELETE FROM sources WHERE id = ?`, id)
	}
}

func (sc *Scanner) cleanup(libraryID string, seenLocals map[string]bool, st *ScanStatus) {
	db := sc.store.db
	rows, err := db.Query(`SELECT sl.id, sl.fs_path FROM series_locals sl
		JOIN series s ON s.id = sl.series_id WHERE s.library_id = ?`, libraryID)
	if err == nil {
		var stale []string
		for rows.Next() {
			var id, p string
			if rows.Scan(&id, &p) == nil && !seenLocals[p] {
				stale = append(stale, id)
			}
		}
		rows.Close()
		for _, id := range stale {
			_, _ = db.Exec(`DELETE FROM series_locals WHERE id = ?`, id)
		}
	}

	orows, err := db.Query(`SELECT id FROM series WHERE library_id = ?
		AND NOT EXISTS (SELECT 1 FROM series_locals sl WHERE sl.series_id = series.id)`, libraryID)
	if err != nil {
		return
	}
	var orphans []string
	for orows.Next() {
		var id string
		if orows.Scan(&id) == nil {
			orphans = append(orphans, id)
		}
	}
	orows.Close()
	for _, id := range orphans {
		if _, err := db.Exec(`DELETE FROM series WHERE id = ?`, id); err == nil {
			if sc.covers != nil {
				sc.covers.Remove(id)
			}
			sc.mutate(st, func(s *ScanStatus) { s.SeriesRemoved++ })
		}
	}
}

// ---------------------------------------------------------------------------
// Per-series processing
// ---------------------------------------------------------------------------

type chEntry struct {
	path   string
	name   string
	format string
	isDir  bool
	size   int64
	mtime  int64
}

type existingCh struct {
	id    string
	size  int64
	mtime int64
}

type todoCh struct {
	entry  chEntry
	source *srcRow
	known  bool
}

type inspectResult struct {
	pages int
	ci    []byte
	size  int64
	err   error
}

type pendingCh struct {
	entry    chEntry
	source   *srcRow
	number   float64
	volume   *float64
	title    string
	pages    int
	size     int64
	released int64
}

func (sc *Scanner) safeProcess(lib *Library, g *scanGroup, st *ScanStatus) {
	defer func() {
		if r := recover(); r != nil {
			sc.addError(st, fmt.Sprintf("series %q: panic: %v", g.norm, r))
		}
	}()
	if err := sc.processGroup(lib, g, st); err != nil {
		sc.addError(st, fmt.Sprintf("series %q: %v", g.norm, err))
	}
}

func dirHasImages(dir string) bool {
	des, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, de := range des {
		if !de.IsDir() && !strings.HasPrefix(de.Name(), ".") && isImageName(de.Name()) {
			return true
		}
	}
	return false
}

func folderSize(dir string) int64 {
	des, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var total int64
	for _, de := range des {
		if de.IsDir() || !isImageName(de.Name()) {
			continue
		}
		if fi, err := de.Info(); err == nil {
			total += fi.Size()
		}
	}
	return total
}

// listChapterEntries lists chapter containers in a series folder: .cbz/.zip/
// .epub/.pdf files and sub-folders that contain images. Unchanged known
// folders skip the (comparatively costly) image check.
func listChapterEntries(dir string, existing map[string]existingCh) ([]chEntry, int, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, err
	}
	var out []chEntry
	unsupported := 0
	for _, de := range des {
		name := de.Name()
		if skipName(name) {
			continue
		}
		full := filepath.Join(dir, name)
		isLink := de.Type()&os.ModeSymlink != 0
		isDir := de.IsDir()
		var fi os.FileInfo
		if isLink || isDir {
			fi, err = os.Stat(full)
			if err != nil {
				continue
			}
			isDir = fi.IsDir()
		}

		if isDir {
			mt := fi.ModTime().Unix()
			if ex, ok := existing[full]; !ok || ex.mtime != mt {
				if !dirHasImages(full) {
					continue
				}
			}
			out = append(out, chEntry{path: full, name: name, format: FormatFolder, isDir: true, mtime: mt})
			continue
		}

		ext := strings.ToLower(filepath.Ext(name))
		format, ok := chapterExts[ext]
		if !ok {
			if unsupportedExts[ext] {
				unsupported++
			}
			continue
		}
		if fi == nil {
			if fi, err = de.Info(); err != nil {
				continue
			}
		}
		out = append(out, chEntry{path: full, name: name, format: format, size: fi.Size(), mtime: fi.ModTime().Unix()})
	}
	return out, unsupported, nil
}

type numberedInfo struct {
	number float64
	info   *ComicInfo
}

func (sc *Scanner) processGroup(lib *Library, g *scanGroup, st *ScanStatus) error {
	db := sc.store.db
	sort.SliceStable(g.locals, func(i, j int) bool {
		a, b := g.locals[i], g.locals[j]
		if a.source.priority != b.source.priority {
			return a.source.priority < b.source.priority
		}
		return strings.ToLower(a.name) < strings.ToLower(b.name)
	})

	// --- Find or create the merged series row ---
	var (
		seriesID                                    string
		locked, coverLocked, metaLocked             int
		oldRating, summary, authors, status, genres string
		tags, coverRel                              string
		year                                        int
		isNew                                       bool
	)
	err := db.QueryRow(`SELECT id, age_locked, age_rating, summary, authors, status, year, genres, tags, cover_rel, cover_locked, meta_locked
		FROM series WHERE library_id = ? AND normalized_title = ?`, lib.ID, g.norm).
		Scan(&seriesID, &locked, &oldRating, &summary, &authors, &status, &year, &genres, &tags, &coverRel, &coverLocked, &metaLocked)
	if errors.Is(err, sql.ErrNoRows) {
		isNew = true
		seriesID = newID()
		title := strings.TrimSpace(g.locals[0].name)
		now := nowUnix()
		if _, err := db.Exec(`INSERT INTO series(id, library_id, title, sort_title, normalized_title, age_rating, added_at, updated_at)
			VALUES(?,?,?,?,?,?,?,?)`, seriesID, lib.ID, title, sortTitle(title), g.norm, RatingTeen, now, now); err != nil {
			return fmt.Errorf("create series: %w", err)
		}
		oldRating = RatingTeen
		sc.mutate(st, func(s *ScanStatus) { s.SeriesAdded++ })
	} else if err != nil {
		return err
	}

	// --- Existing chapters of this series ---
	existing := map[string]existingCh{}
	if rows, err := db.Query(`SELECT id, fs_path, size_bytes, mtime FROM chapters WHERE series_id = ?`, seriesID); err == nil {
		for rows.Next() {
			var id, p string
			var size, mt int64
			if rows.Scan(&id, &p, &size, &mt) == nil {
				existing[p] = existingCh{id: id, size: size, mtime: mt}
			}
		}
		rows.Close()
	}

	// --- Walk every physical folder ---
	var (
		upserts    []pendingCh
		todo       []todoCh
		infos      []numberedInfo
		seen       = map[string]bool{}
		listErr    bool
		total      int
		hashPrefix int
		skipped    int
		unsup      int
	)
	for _, loc := range g.locals {
		entries, u, err := listChapterEntries(loc.path, existing)
		if err != nil {
			sc.addError(st, fmt.Sprintf("list %s: %v", loc.path, err))
			listErr = true
			continue
		}
		unsup += u
		for _, en := range entries {
			total++
			if strings.HasPrefix(en.name, "#") {
				hashPrefix++
			}
			ex, known := existing[en.path]
			if known && ex.mtime == en.mtime && (en.isDir || ex.size == en.size) {
				seen[en.path] = true
				skipped++
				continue
			}
			todo = append(todo, todoCh{entry: en, source: loc.source, known: known})
		}
	}

	// Inspect new/changed chapters concurrently: on network shares each archive
	// open is latency-bound, so parallelism (bounded by a scanner-wide
	// semaphore) dominates scan time.
	results := make([]inspectResult, len(todo))
	var iwg sync.WaitGroup
	for i := range todo {
		iwg.Add(1)
		sc.inspectSem <- struct{}{}
		go func(i int) {
			defer iwg.Done()
			defer func() { <-sc.inspectSem }()
			defer func() {
				if r := recover(); r != nil {
					results[i].err = fmt.Errorf("panic: %v", r)
				}
			}()
			en := todo[i].entry
			pages, ci, err := InspectChapter(en.path, en.format)
			results[i] = inspectResult{pages: pages, ci: ci, err: err}
			if err == nil && en.isDir {
				results[i].size = folderSize(en.path)
			}
		}(i)
	}
	iwg.Wait()

	for i, t := range todo {
		en, res := t.entry, results[i]
		if res.err != nil || res.pages == 0 {
			err := res.err
			if err == nil {
				err = ErrNoPages
			}
			sc.addError(st, fmt.Sprintf("%s: %v", en.path, err))
			if t.known {
				seen[en.path] = true // keep the stale row rather than lose progress targets
			}
			continue
		}
		seen[en.path] = true

		number, volume, title := parseChapterName(en.name, !en.isDir)
		var released int64
		if res.ci != nil {
			if ci, err := parseComicInfo(res.ci); err == nil {
				if number < 0 {
					number = ci.number()
				}
				if t := strings.TrimSpace(ci.Title); t != "" {
					title = t
				}
				if d := ci.date(); !d.IsZero() {
					released = d.Unix()
				}
				infos = append(infos, numberedInfo{number: number, info: ci})
			}
		}
		if released == 0 {
			released = en.mtime
		}
		size := en.size
		if en.isDir {
			size = res.size
		}
		upserts = append(upserts, pendingCh{entry: en, source: t.source, number: number, volume: volume,
			title: title, pages: res.pages, size: size, released: released})
	}

	// --- Persist ---
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	localPaths := map[string]bool{}
	for _, loc := range g.locals {
		localPaths[loc.path] = true
		if _, err := tx.Exec(`INSERT INTO series_locals(id, series_id, source_id, local_title, fs_path) VALUES(?,?,?,?,?)
			ON CONFLICT(fs_path) DO UPDATE SET series_id = excluded.series_id, source_id = excluded.source_id,
			local_title = excluded.local_title`, newID(), seriesID, loc.source.id, loc.name, loc.path); err != nil {
			return fmt.Errorf("upsert local: %w", err)
		}
	}
	// Locals of this series that vanished from disk.
	if rows, err := tx.Query(`SELECT id, fs_path FROM series_locals WHERE series_id = ?`, seriesID); err == nil {
		var stale []string
		for rows.Next() {
			var id, p string
			if rows.Scan(&id, &p) == nil && !localPaths[p] {
				stale = append(stale, id)
			}
		}
		rows.Close()
		for _, id := range stale {
			if _, err := tx.Exec(`DELETE FROM series_locals WHERE id = ?`, id); err != nil {
				return err
			}
		}
	}

	now := nowUnix()
	added, updated, removed := 0, 0, 0
	for _, p := range upserts {
		var vol any
		if p.volume != nil {
			vol = *p.volume
		}
		if _, known := existing[p.entry.path]; known {
			updated++
		} else {
			added++
		}
		if _, err := tx.Exec(`INSERT INTO chapters(id, series_id, source_id, number, volume, title, fs_path, format,
			page_count, size_bytes, mtime, released_at, scanned_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(fs_path) DO UPDATE SET series_id = excluded.series_id, source_id = excluded.source_id,
			number = excluded.number, volume = excluded.volume, title = excluded.title, format = excluded.format,
			page_count = excluded.page_count, size_bytes = excluded.size_bytes, mtime = excluded.mtime,
			released_at = excluded.released_at, scanned_at = excluded.scanned_at`,
			newID(), seriesID, p.source.id, p.number, vol, p.title, p.entry.path, p.entry.format,
			p.pages, p.size, p.entry.mtime, p.released, now); err != nil {
			return fmt.Errorf("upsert chapter %s: %w", p.entry.path, err)
		}
	}
	if !listErr {
		for p, ex := range existing {
			if !seen[p] {
				if _, err := tx.Exec(`DELETE FROM chapters WHERE id = ?`, ex.id); err != nil {
					return err
				}
				removed++
			}
		}
	}

	// --- Series metadata (only fills empty fields) ---
	sort.SliceStable(infos, func(i, j int) bool {
		a, b := infos[i].number, infos[j].number
		if (a < 0) != (b < 0) {
			return a >= 0
		}
		return a < b
	})
	cis := make([]*ComicInfo, len(infos))
	for i, ni := range infos {
		cis[i] = ni.info
	}
	meta := mergeComicInfo(cis)
	if metaLocked == 0 {
		if summary == "" {
			summary = meta.Summary
		}
		if authors == "" {
			authors = meta.Authors
		}
		if status == "" {
			status = meta.Status
		}
		if year == 0 {
			year = meta.Year
		}
		if genres == "" {
			genres = joinCSV(meta.Genres)
		}
		if tags == "" {
			tags = joinCSV(meta.Tags)
		}
	}

	srcNames := make([]string, 0, len(g.locals))
	for _, l := range g.locals {
		srcNames = append(srcNames, l.source.name)
	}
	ratio := 0.0
	if total > 0 {
		ratio = float64(hashPrefix) / float64(total)
	}
	rating := inferAgeRating(srcNames, splitCSV(genres), splitCSV(tags), ratio, meta.Ratings)
	if len(meta.Ratings) == 0 {
		// No ComicInfo seen this run (incremental scan): never loosen a rating.
		rating = maxRating(rating, normalizeRatingOr(oldRating, RatingTeen))
	}

	var chapterCount int
	if err := tx.QueryRow(`SELECT COUNT(DISTINCT CASE WHEN number >= 0 THEN number END)
		+ COALESCE(SUM(CASE WHEN number < 0 THEN 1 ELSE 0 END), 0) FROM chapters WHERE series_id = ?`, seriesID).
		Scan(&chapterCount); err != nil {
		return err
	}
	changed := isNew || added+updated+removed > 0
	if _, err := tx.Exec(`UPDATE series SET summary = ?, authors = ?, status = ?, year = ?, genres = ?, tags = ?,
		age_rating = CASE WHEN age_locked = 1 THEN age_rating ELSE ? END, chapter_count = ?,
		updated_at = CASE WHEN ? THEN ? ELSE updated_at END WHERE id = ?`,
		summary, authors, status, year, genres, tags, rating, chapterCount, changed, now, seriesID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	sc.mutate(st, func(s *ScanStatus) {
		s.ChaptersAdded += added
		s.ChaptersUpdated += updated
		s.ChaptersRemoved += removed
		s.ChaptersSkipped += skipped
		s.Unsupported += unsup
	})

	if sc.covers != nil && coverLocked == 0 && chapterCount > 0 && (coverRel == "" || !sc.covers.Has(seriesID)) {
		sc.covers.Enqueue(seriesID)
	}
	return nil
}

func normalizeRatingOr(s, def string) string {
	if r := normalizeRating(s); r != "" {
		return r
	}
	return def
}
