package library

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"yomi-reader/auth"
)

// Handler serves the catalog/reader JSON API. Admin endpoints live in admin.go
// and OPDS in opds.go.
type Handler struct {
	store   *Store
	scanner *Scanner
	covers  *CoverService
	auto    *AutoScanner
}

// NewHandler wires the handler to its services.
func NewHandler(store *Store, scanner *Scanner, covers *CoverService) *Handler {
	return &Handler{store: store, scanner: scanner, covers: covers}
}

// SetAutoScanner attaches the auto-scanner (used by the settings endpoints).
func (h *Handler) SetAutoScanner(a *AutoScanner) { h.auto = a }

// Store exposes the underlying store (for wiring in main/server).
func (h *Handler) Store() *Store { return h.store }

// RecordLogin updates the user table after a successful login. Passwordless
// providers (mock, plex) create/refresh the account; local accounts only get a
// last-seen timestamp.
func (h *Handler) RecordLogin(u *auth.User, provider string) error {
	if provider == "mock" || provider == "plex" {
		return h.store.UpsertExternalUser(u.Username, u.Email, u.IsAdmin)
	}
	h.store.TouchLastSeen(u.Username)
	return nil
}

// viewer resolves the authenticated viewer (fresh from the DB). On failure it
// has already written the 401 response.
func (h *Handler) viewer(w http.ResponseWriter, r *http.Request) (Viewer, bool) {
	s := auth.FromContext(r.Context())
	if s == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return Viewer{}, false
	}
	v, err := h.store.ViewerFor(s.Username)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return Viewer{}, false
	}
	return v, true
}

func (h *Handler) internalErr(w http.ResponseWriter, err error) {
	writeErr(w, http.StatusInternalServerError, err.Error())
}

func chapterDisplayTitle(c *Chapter) {
	ensureEdition(c)
	if ed, cleaned := splitEditionTitle(c.Title); ed != "" {
		if c.Edition == "" {
			c.Edition = ed
		}
		c.Title = cleaned
	}
	if strings.TrimSpace(c.Title) != "" {
		return
	}
	if c.Number >= 0 {
		c.Title = "Chapter " + strconv.FormatFloat(c.Number, 'f', -1, 64)
	} else {
		c.Title = "Untitled"
	}
}

// ---------------------------------------------------------------------------
// Me / preferences / libraries
// ---------------------------------------------------------------------------

// Me returns the current account.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	v, ok := h.viewer(w, r)
	if !ok {
		return
	}
	u, err := h.store.GetUser(v.Username)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	libs, err := h.store.ListLibrariesForUser(v)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	type libRef struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		SeriesCount int    `json:"seriesCount"`
	}
	refs := make([]libRef, 0, len(libs))
	for _, l := range libs {
		refs = append(refs, libRef{l.ID, l.Name, l.SeriesCount})
	}
	writeJSONOK(w, map[string]any{
		"username":     u.Username,
		"email":        u.Email,
		"isAdmin":      u.IsAdmin,
		"maxAgeRating": u.MaxAgeRating,
		"libraries":    refs,
	})
}

// ListLibraries returns the libraries visible to the user.
func (h *Handler) ListLibraries(w http.ResponseWriter, r *http.Request) {
	v, ok := h.viewer(w, r)
	if !ok {
		return
	}
	libs, err := h.store.ListLibrariesForUser(v)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSONOK(w, libs)
}

func (h *Handler) userPrefs(username string) (map[string]any, error) {
	raw, err := h.store.GetPrefs(username)
	if err != nil {
		return nil, err
	}
	prefs := map[string]any{}
	_ = json.Unmarshal([]byte(raw), &prefs)
	delete(prefs, "readerMode") // migrated to per-series preferences
	return prefs, nil
}

// resolveReaderMode returns the user's mode for a series, falling back to the
// admin default (and finally "webtoon").
func (h *Handler) resolveReaderMode(username, seriesID string) (string, error) {
	mode, err := h.store.GetSeriesReaderMode(username, seriesID)
	if err != nil {
		return "", err
	}
	if ValidSeriesReaderModes[mode] {
		return mode, nil
	}
	def, _ := h.store.GetSetting(SettingDefaultReaderMode, "auto")
	if ValidSeriesReaderModes[def] {
		return def, nil
	}
	return "webtoon", nil
}

// GetPreferences returns the user's preferences (language, etc.).
func (h *Handler) GetPreferences(w http.ResponseWriter, r *http.Request) {
	v, ok := h.viewer(w, r)
	if !ok {
		return
	}
	prefs, err := h.userPrefs(v.Username)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSONOK(w, prefs)
}

// PutPreferences replaces the user's preferences with the posted JSON object.
func (h *Handler) PutPreferences(w http.ResponseWriter, r *http.Request) {
	v, ok := h.viewer(w, r)
	if !ok {
		return
	}
	var in map[string]any
	if !decodeJSON(w, r, &in) {
		return
	}
	if in == nil {
		in = map[string]any{}
	}
	delete(in, "readerMode") // reader mode is per-series
	b, err := json.Marshal(in)
	if err != nil || len(b) > 64<<10 {
		writeErr(w, http.StatusBadRequest, "preferences too large")
		return
	}
	if err := h.store.SetPrefs(v.Username, string(b)); err != nil {
		h.internalErr(w, err)
		return
	}
	prefs, _ := h.userPrefs(v.Username)
	writeJSONOK(w, prefs)
}

// ---------------------------------------------------------------------------
// Resume logic
// ---------------------------------------------------------------------------

// Resume tells the client which chapter/page to open to continue a series.
type Resume struct {
	ChapterID     string  `json:"chapterId"`
	ChapterNumber float64 `json:"chapterNumber"`
	ChapterTitle  string  `json:"chapterTitle"`
	PageIndex     int     `json:"pageIndex"`
	ScrollRatio   float64 `json:"scrollRatio"`
	Finished      bool    `json:"finished"` // everything read, nothing newer to open
}

// resolveResume maps stored progress onto the current merged chapter list. A
// completed chapter resumes at the start of the next one; a vanished chapter
// resumes at the first chapter numbered at or after it.
func resolveResume(merged []*Chapter, p *Progress) *Resume {
	if p == nil || len(merged) == 0 {
		return nil
	}
	idx := locateChapter(merged, p.ChapterID, p.ChapterNumber)
	page, scroll := p.PageIndex, p.ScrollRatio
	chosenID := ""
	if idx < 0 {
		idx = 0
		if p.ChapterNumber >= 0 {
			for i, c := range merged {
				if c.Number >= p.ChapterNumber {
					idx = i
					break
				}
			}
		}
		page, scroll = 0, 0
	} else if merged[idx].ID != p.ChapterID {
		// Progress points at an alternative copy: keep it if it still exists,
		// since page indexes are relative to that file.
		for _, a := range merged[idx].Alts {
			if a.ID == p.ChapterID {
				chosenID = a.ID
			}
		}
	} else {
		chosenID = p.ChapterID
	}

	if p.Completed {
		if idx+1 >= len(merged) {
			c := merged[idx]
			return &Resume{ChapterID: c.ID, ChapterNumber: c.Number, ChapterTitle: c.Title, Finished: true}
		}
		idx, page, scroll, chosenID = idx+1, 0, 0, ""
	}
	c := merged[idx]
	if chosenID == "" {
		chosenID = c.ID
	}
	return &Resume{ChapterID: chosenID, ChapterNumber: c.Number, ChapterTitle: c.Title, PageIndex: page, ScrollRatio: scroll}
}

func (h *Handler) mergedChapters(seriesID string) ([]*Chapter, error) {
	all, err := h.store.ListChaptersEffective(seriesID)
	if err != nil {
		return nil, err
	}
	merged := MergeChapters(all)
	for _, c := range merged {
		chapterDisplayTitle(c)
	}
	return merged, nil
}

// ---------------------------------------------------------------------------
// Home
// ---------------------------------------------------------------------------

// ContinueItem is one "continue reading" card.
type ContinueItem struct {
	Series        *Series   `json:"series"`
	ChapterID     string    `json:"chapterId"`
	ChapterNumber float64   `json:"chapterNumber"`
	ChapterTitle  string    `json:"chapterTitle"`
	PageIndex     int       `json:"pageIndex"`
	ScrollRatio   float64   `json:"scrollRatio"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// parseAgeMode reads ?ageMode= from the request. Empty means no extra filter.
// Returns false after writing an error response when the value is invalid or
// the viewer may not use "adult".
func (h *Handler) parseAgeMode(w http.ResponseWriter, r *http.Request, v Viewer) (string, bool) {
	mode := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("ageMode")))
	switch mode {
	case "", "all":
		return "", true
	case "safe":
		return "safe", true
	case "adult", "x":
		if !v.CanSeeRating(RatingExplicit) {
			writeErr(w, http.StatusForbidden, "adult catalog requires max age rating X")
			return "", false
		}
		return "adult", true
	default:
		writeErr(w, http.StatusBadRequest, "invalid ageMode (use safe, adult, or all)")
		return "", false
	}
}

// Home returns the personalised swimlanes.
func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {
	v, ok := h.viewer(w, r)
	if !ok {
		return
	}
	ageMode, ok := h.parseAgeMode(w, r, v)
	if !ok {
		return
	}

	progress, err := h.store.ListProgress(v, 40, ageMode)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	cont := []ContinueItem{}
	for _, p := range progress {
		if len(cont) >= 20 {
			break
		}
		se, err := h.store.GetSeries(v, p.SeriesID)
		if err != nil {
			continue
		}
		merged, err := h.mergedChapters(p.SeriesID)
		if err != nil {
			continue
		}
		rs := resolveResume(merged, p)
		if rs == nil || rs.Finished {
			continue
		}
		cont = append(cont, ContinueItem{Series: se, ChapterID: rs.ChapterID, ChapterNumber: rs.ChapterNumber,
			ChapterTitle: rs.ChapterTitle, PageIndex: rs.PageIndex, ScrollRatio: rs.ScrollRatio, UpdatedAt: p.UpdatedAt})
	}

	favs, err := h.store.ListFavorites(v, 30, ageMode)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	added, err := h.store.ListNewest(v, "added", 30, ageMode)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	updated, err := h.store.ListNewest(v, "updated", 30, ageMode)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	// Stable within the configured window so refresh doesn't reshuffle every time.
	random, err := h.store.ListRandom(v, 30, TimeBucketSeed(time.Duration(h.randomRefreshMinutes())*time.Minute), ageMode)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSONOK(w, map[string]any{
		"continue":        cont,
		"favorites":       favs,
		"recentlyAdded":   added,
		"recentlyUpdated": updated,
		"random":          random,
	})
}

// ---------------------------------------------------------------------------
// Series
// ---------------------------------------------------------------------------

// ListSeries is the catalog/search endpoint.
func (h *Handler) ListSeries(w http.ResponseWriter, r *http.Request) {
	v, ok := h.viewer(w, r)
	if !ok {
		return
	}
	ageMode, ok := h.parseAgeMode(w, r, v)
	if !ok {
		return
	}
	qp := r.URL.Query()
	var tags []string
	for _, t := range qp["tag"] {
		tags = append(tags, splitCSV(t)...)
	}
	q := SeriesQuery{
		Q:             qp.Get("q"),
		Tags:          tags,
		Source:        qp.Get("source"),
		Age:           qp.Get("age"),
		AgeMode:       ageMode,
		Sort:          qp.Get("sort"),
		Order:         qp.Get("order"),
		Offset:        queryInt(r, "offset", 0),
		Limit:         queryInt(r, "limit", 48),
		LibraryID:     qp.Get("library"),
		OnlyFavorites: qp.Get("favorites") == "1" || qp.Get("favorites") == "true",
	}
	if q.Age != "" && normalizeRating(q.Age) == "" {
		writeErr(w, http.StatusBadRequest, "invalid age (use E, T, M or X)")
		return
	}
	page, err := h.store.ListSeries(v, q)
	if err != nil {
		if errors.Is(err, ErrForbidden) {
			writeErr(w, http.StatusForbidden, "adult catalog requires max age rating X")
			return
		}
		h.internalErr(w, err)
		return
	}
	writeJSONOK(w, page)
}

// SourceInfo summarises one source's contribution to a series.
type SourceInfo struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Priority     int    `json:"priority"`
	ChapterCount int    `json:"chapterCount"`
}

// SeriesDetail is the GET /api/series/{id} payload.
type SeriesDetail struct {
	*Series
	Progress       *Progress    `json:"progress"`
	Resume         *Resume      `json:"resume"`
	ReaderMode     string       `json:"readerMode"`
	SourceDetails  []SourceInfo `json:"sourceDetails"`
	FirstChapterID string       `json:"firstChapterId,omitempty"`
	LastChapterID  string       `json:"lastChapterId,omitempty"`
}

// GetSeries returns series detail with the user's progress and resume target.
func (h *Handler) GetSeries(w http.ResponseWriter, r *http.Request) {
	v, ok := h.viewer(w, r)
	if !ok {
		return
	}
	se, err := h.store.GetSeries(v, r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		writeErr(w, http.StatusNotFound, "series not found")
		return
	}
	if err != nil {
		h.internalErr(w, err)
		return
	}
	all, err := h.store.ListChaptersEffective(se.ID)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	merged := MergeChapters(all)
	for _, c := range merged {
		chapterDisplayTitle(c)
	}

	d := SeriesDetail{Series: se, SourceDetails: []SourceInfo{}}
	if mode, err := h.resolveReaderMode(v.Username, se.ID); err == nil {
		d.ReaderMode = mode
	} else {
		d.ReaderMode = "webtoon"
	}
	perSource := map[string]*SourceInfo{}
	var order []string
	for _, c := range all {
		si, ok := perSource[c.SourceID]
		if !ok {
			si = &SourceInfo{ID: c.SourceID, Name: c.SourceName, Priority: c.Priority}
			perSource[c.SourceID] = si
			order = append(order, c.SourceID)
		}
		si.ChapterCount++
	}
	for _, id := range order {
		d.SourceDetails = append(d.SourceDetails, *perSource[id])
	}
	sortSourceInfos(d.SourceDetails)
	if len(merged) > 0 {
		d.FirstChapterID = merged[0].ID
		d.LastChapterID = merged[len(merged)-1].ID
	}
	if p, err := h.store.GetProgress(v.Username, se.ID); err == nil && p != nil {
		d.Progress = p
		d.Resume = resolveResume(merged, p)
	}
	writeJSONOK(w, d)
}

func sortSourceInfos(s []SourceInfo) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && (s[j].Priority < s[j-1].Priority ||
			(s[j].Priority == s[j-1].Priority && strings.ToLower(s[j].Name) < strings.ToLower(s[j-1].Name))); j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// ListChapters returns the merged chapter list (best source per number), all
// copies with ?allSources=1, or volume groups with ?group=volume.
func (h *Handler) ListChapters(w http.ResponseWriter, r *http.Request) {
	v, ok := h.viewer(w, r)
	if !ok {
		return
	}
	se, err := h.store.GetSeries(v, r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		writeErr(w, http.StatusNotFound, "series not found")
		return
	}
	if err != nil {
		h.internalErr(w, err)
		return
	}
	all, err := h.store.ListChaptersEffective(se.ID)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	qp := r.URL.Query()
	if qp.Get("allSources") == "1" || qp.Get("allSources") == "true" {
		SortChapters(all)
		for _, c := range all {
			chapterDisplayTitle(c)
		}
		writeJSONOK(w, all)
		return
	}
	merged := MergeChapters(all)
	for _, c := range merged {
		chapterDisplayTitle(c)
	}
	if qp.Get("group") == "volume" {
		writeJSONOK(w, GroupByVolume(merged))
		return
	}
	writeJSONOK(w, merged)
}

// ---------------------------------------------------------------------------
// Chapters
// ---------------------------------------------------------------------------

// ChapterLink is a lightweight pointer to a neighbouring chapter.
type ChapterLink struct {
	ID     string  `json:"id"`
	Number float64 `json:"number"`
	Title  string  `json:"title"`
}

// ChapterDetail is the GET /api/chapters/{id} payload.
type ChapterDetail struct {
	*Chapter
	SeriesTitle string       `json:"seriesTitle"`
	Prev        *ChapterLink `json:"prev"`
	Next        *ChapterLink `json:"next"`
}

// loadChapter loads a chapter, enforces library/age access (404 otherwise) and
// returns the confined absolute path of its file.
func (h *Handler) loadChapter(w http.ResponseWriter, r *http.Request) (*ChapterRef, string, Viewer, bool) {
	v, ok := h.viewer(w, r)
	if !ok {
		return nil, "", v, false
	}
	ref, err := h.store.GetChapterRef(r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		writeErr(w, http.StatusNotFound, "chapter not found")
		return nil, "", v, false
	}
	if err != nil {
		h.internalErr(w, err)
		return nil, "", v, false
	}
	allowed, err := h.store.CanViewChapter(v, ref)
	if err != nil {
		h.internalErr(w, err)
		return nil, "", v, false
	}
	if !allowed {
		writeErr(w, http.StatusNotFound, "chapter not found")
		return nil, "", v, false
	}
	full, err := ResolveUnderRoot(ref.LibraryRoot, ref.FSPath)
	if err != nil {
		writeErr(w, http.StatusForbidden, "chapter path is outside the library")
		return nil, "", v, false
	}
	chapterDisplayTitle(&ref.Chapter)
	return ref, full, v, true
}

func (h *Handler) archiveError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		writeErr(w, http.StatusNotFound, "chapter file is missing on disk")
	case errors.Is(err, ErrPageOutOfRange):
		writeErr(w, http.StatusNotFound, "page out of range")
	case errors.Is(err, ErrNoPages), errors.Is(err, zip.ErrFormat):
		writeErr(w, http.StatusUnprocessableEntity, "chapter archive is unreadable or has no image pages")
	default:
		h.internalErr(w, err)
	}
}

// GetChapter returns chapter metadata with prev/next neighbours.
func (h *Handler) GetChapter(w http.ResponseWriter, r *http.Request) {
	ref, _, _, ok := h.loadChapter(w, r)
	if !ok {
		return
	}
	all, err := h.store.ListChaptersEffective(ref.SeriesID)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	merged := MergeChapters(all)
	for _, c := range merged {
		chapterDisplayTitle(c)
	}
	d := ChapterDetail{Chapter: &ref.Chapter}
	_ = h.store.db.QueryRow(`SELECT title FROM series WHERE id = ?`, ref.SeriesID).Scan(&d.SeriesTitle)

	// Other copies of the same chapter number (other sources / editions).
	if ref.Number >= 0 {
		k := numKey(ref.Number)
		for _, c := range all {
			if c.ID != ref.ID && c.Number >= 0 && numKey(c.Number) == k {
				d.Alts = append(d.Alts, chapterAlt(c))
			}
		}
	}
	if idx := locateChapter(merged, ref.ID, ref.Number); idx >= 0 {
		if idx > 0 {
			p := merged[idx-1]
			d.Prev = &ChapterLink{ID: p.ID, Number: p.Number, Title: p.Title}
		}
		if idx+1 < len(merged) {
			n := merged[idx+1]
			d.Next = &ChapterLink{ID: n.ID, Number: n.Number, Title: n.Title}
		}
	}
	writeJSONOK(w, d)
}

// ChapterPages lists the pages of a chapter as [{index,name}].
func (h *Handler) ChapterPages(w http.ResponseWriter, r *http.Request) {
	ref, full, _, ok := h.loadChapter(w, r)
	if !ok {
		return
	}
	names, err := ListPages(full, ref.Format)
	if err != nil {
		h.archiveError(w, err)
		return
	}
	type page struct {
		Index int    `json:"index"`
		Name  string `json:"name"`
	}
	out := make([]page, len(names))
	for i, n := range names {
		out[i] = page{Index: i, Name: n}
	}
	writeJSONOK(w, out)
}

// ChapterPage streams one page image (or the whole PDF for page 0 of a PDF).
func (h *Handler) ChapterPage(w http.ResponseWriter, r *http.Request) {
	ref, full, _, ok := h.loadChapter(w, r)
	if !ok {
		return
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 0 {
		writeErr(w, http.StatusBadRequest, "invalid page number")
		return
	}

	etag := fmt.Sprintf(`W/"%s-%d-%d-%d"`, ref.ID, n, ref.MTime, ref.SizeBytes)
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	rc, ctype, size, err := OpenPage(full, ref.Format, n)
	if err != nil {
		h.archiveError(w, err)
		return
	}
	defer rc.Close()

	hd := w.Header()
	hd.Set("Content-Type", ctype)
	hd.Set("Cache-Control", "private, max-age=86400")
	hd.Set("ETag", etag)
	hd.Set("X-Content-Type-Options", "nosniff")
	if size >= 0 {
		hd.Set("Content-Length", strconv.FormatInt(size, 10))
	}
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, rc)
}

// ChapterFile serves the raw chapter file: cbz/epub/pdf with Range support, or
// an on-the-fly CBZ for folder chapters. ?download=1 forces an attachment.
func (h *Handler) ChapterFile(w http.ResponseWriter, r *http.Request) {
	ref, full, _, ok := h.loadChapter(w, r)
	if !ok {
		return
	}
	base := safeFileName(ref.Title)
	download := r.URL.Query().Get("download") == "1"
	disp := "inline"
	if download || ref.Format != FormatPDF {
		disp = "attachment"
	}

	if ref.Format == FormatFolder {
		names, err := listFolderImages(full)
		if err != nil {
			h.archiveError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.comicbook+zip")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": base + ".cbz"}))
		zw := zip.NewWriter(w)
		for _, n := range names {
			f, err := os.Open(filepath.Join(full, n))
			if err != nil {
				continue
			}
			ew, err := zw.CreateHeader(&zip.FileHeader{Name: n, Method: zip.Store})
			if err == nil {
				_, _ = io.Copy(ew, f)
			}
			f.Close()
			if err != nil {
				break
			}
		}
		_ = zw.Close()
		return
	}

	f, err := os.Open(full)
	if err != nil {
		h.archiveError(w, err)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	ext := map[string]string{FormatCBZ: ".cbz", FormatEPUB: ".epub", FormatPDF: ".pdf"}[ref.Format]
	ctype := map[string]string{FormatCBZ: "application/vnd.comicbook+zip", FormatEPUB: "application/epub+zip", FormatPDF: "application/pdf"}[ref.Format]
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disp, map[string]string{"filename": base + ext}))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, base+ext, st.ModTime(), f)
}

func safeFileName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`\/:*?"<>|`, r) {
			return '_'
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if s == "" {
		return "chapter"
	}
	return s
}

// ---------------------------------------------------------------------------
// Covers
// ---------------------------------------------------------------------------

// Cover serves the cached (or lazily generated) series cover.
func (h *Handler) Cover(w http.ResponseWriter, r *http.Request) {
	v, ok := h.viewer(w, r)
	if !ok {
		return
	}
	se, err := h.store.GetSeries(v, r.PathValue("seriesId"))
	if errors.Is(err, ErrNotFound) {
		writeErr(w, http.StatusNotFound, "series not found")
		return
	}
	if err != nil {
		h.internalErr(w, err)
		return
	}
	p, err := h.covers.Ensure(se.ID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "no cover available")
		return
	}
	f, err := os.Open(p)
	if err != nil {
		writeErr(w, http.StatusNotFound, "no cover available")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	// Short cache so manual cover overrides show up quickly; ServeContent still
	// sends Last-Modified / ETag for conditional revalidation.
	w.Header().Set("Cache-Control", "private, max-age=60, must-revalidate")
	http.ServeContent(w, r, se.ID+".jpg", st.ModTime(), f)
}

// ---------------------------------------------------------------------------
// Progress and favorites
// ---------------------------------------------------------------------------

type progressResponse struct {
	Progress *Progress `json:"progress"`
	Resume   *Resume   `json:"resume"`
}

func (h *Handler) progressFor(username, seriesID string) (*progressResponse, error) {
	p, err := h.store.GetProgress(username, seriesID)
	if err != nil {
		return nil, err
	}
	out := &progressResponse{Progress: p}
	if p != nil {
		merged, err := h.mergedChapters(seriesID)
		if err != nil {
			return nil, err
		}
		out.Resume = resolveResume(merged, p)
	}
	return out, nil
}

func (h *Handler) seriesOr404(w http.ResponseWriter, r *http.Request) (Viewer, *Series, bool) {
	v, ok := h.viewer(w, r)
	if !ok {
		return v, nil, false
	}
	se, err := h.store.GetSeries(v, r.PathValue("seriesId"))
	if errors.Is(err, ErrNotFound) {
		writeErr(w, http.StatusNotFound, "series not found")
		return v, nil, false
	}
	if err != nil {
		h.internalErr(w, err)
		return v, nil, false
	}
	return v, se, true
}

// GetProgress returns {progress, resume} for a series.
func (h *Handler) GetProgress(w http.ResponseWriter, r *http.Request) {
	v, se, ok := h.seriesOr404(w, r)
	if !ok {
		return
	}
	out, err := h.progressFor(v.Username, se.ID)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSONOK(w, out)
}

// PutProgress stores the reading position of a series.
func (h *Handler) PutProgress(w http.ResponseWriter, r *http.Request) {
	v, se, ok := h.seriesOr404(w, r)
	if !ok {
		return
	}
	var in struct {
		ChapterID   string  `json:"chapterId"`
		PageIndex   int     `json:"pageIndex"`
		ScrollRatio float64 `json:"scrollRatio"`
		Completed   bool    `json:"completed"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	ref, err := h.store.GetChapterRef(in.ChapterID)
	if errors.Is(err, ErrNotFound) || (err == nil && ref.SeriesID != se.ID) {
		writeErr(w, http.StatusBadRequest, "chapterId does not belong to this series")
		return
	}
	if err != nil {
		h.internalErr(w, err)
		return
	}
	page := in.PageIndex
	if page < 0 {
		page = 0
	}
	if ref.PageCount > 0 && page > ref.PageCount-1 {
		page = ref.PageCount - 1
	}
	scroll := in.ScrollRatio
	if scroll < 0 || scroll != scroll { // NaN guard
		scroll = 0
	}
	if scroll > 1 {
		scroll = 1
	}
	p := &Progress{SeriesID: se.ID, ChapterID: ref.ID, ChapterNumber: ref.Number,
		PageIndex: page, ScrollRatio: scroll, Completed: in.Completed}
	if err := h.store.PutProgress(v.Username, p); err != nil {
		h.internalErr(w, err)
		return
	}
	out, err := h.progressFor(v.Username, se.ID)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSONOK(w, out)
}

// DeleteProgress clears the reading position ("mark as unread").
func (h *Handler) DeleteProgress(w http.ResponseWriter, r *http.Request) {
	v, se, ok := h.seriesOr404(w, r)
	if !ok {
		return
	}
	if _, err := h.store.db.Exec(`DELETE FROM reading_progress WHERE username = ? AND series_id = ?`, v.Username, se.ID); err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSONOK(w, map[string]bool{"ok": true})
}

// SetFavorite handles PUT (add) and DELETE (remove) on /api/favorites/{seriesId}.
func (h *Handler) SetFavorite(w http.ResponseWriter, r *http.Request) {
	v, se, ok := h.seriesOr404(w, r)
	if !ok {
		return
	}
	on := r.Method != http.MethodDelete
	if err := h.store.SetFavorite(v.Username, se.ID, on); err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSONOK(w, map[string]bool{"favorite": on})
}

// PutSeriesReaderMode stores the user's reader mode for a series.
func (h *Handler) PutSeriesReaderMode(w http.ResponseWriter, r *http.Request) {
	v, ok := h.viewer(w, r)
	if !ok {
		return
	}
	se, err := h.store.GetSeries(v, r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		writeErr(w, http.StatusNotFound, "series not found")
		return
	}
	if err != nil {
		h.internalErr(w, err)
		return
	}
	var in struct {
		ReaderMode string `json:"readerMode"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !ValidSeriesReaderModes[in.ReaderMode] {
		writeErr(w, http.StatusBadRequest, "readerMode must be one of ltr, rtl, vertical, webtoon")
		return
	}
	if err := h.store.SetSeriesReaderMode(v.Username, se.ID, in.ReaderMode); err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSONOK(w, map[string]string{"readerMode": in.ReaderMode})
}

// ListFavorites returns the user's favorite series.
func (h *Handler) ListFavorites(w http.ResponseWriter, r *http.Request) {
	v, ok := h.viewer(w, r)
	if !ok {
		return
	}
	ageMode, ok := h.parseAgeMode(w, r, v)
	if !ok {
		return
	}
	list, err := h.store.ListFavorites(v, 500, ageMode)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSONOK(w, list)
}

// ---------------------------------------------------------------------------
// Tags / sources
// ---------------------------------------------------------------------------

// ListTags returns genres+tags with counts for the viewer's visible series.
func (h *Handler) ListTags(w http.ResponseWriter, r *http.Request) {
	v, ok := h.viewer(w, r)
	if !ok {
		return
	}
	ageMode, ok := h.parseAgeMode(w, r, v)
	if !ok {
		return
	}
	tags, err := h.store.ListTags(v, ageMode)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSONOK(w, tags)
}

// ListSources returns sources (with priority and visible series counts).
func (h *Handler) ListSources(w http.ResponseWriter, r *http.Request) {
	v, ok := h.viewer(w, r)
	if !ok {
		return
	}
	ageMode, ok := h.parseAgeMode(w, r, v)
	if !ok {
		return
	}
	sources, err := h.store.ListSources(v, ageMode)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSONOK(w, sources)
}
