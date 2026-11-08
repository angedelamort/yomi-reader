package library

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/disintegration/imaging"
	"yomi-reader/auth"
)

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9._@+-]{1,64}$`)

// ---------------------------------------------------------------------------
// Directory browser
// ---------------------------------------------------------------------------

type browseEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// AdminBrowseDirs lists sub-directories of ?path= (drive letters on Windows
// when empty) so the UI can pick a library root. It is read-only.
func (h *Handler) AdminBrowseDirs(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimSpace(r.URL.Query().Get("path"))
	resp := map[string]any{"separator": string(filepath.Separator)}

	if p == "" {
		entries := []browseEntry{}
		if runtime.GOOS == "windows" {
			for c := 'A'; c <= 'Z'; c++ {
				root := string(c) + `:\`
				if _, err := os.Stat(root); err == nil {
					entries = append(entries, browseEntry{Name: string(c) + ":", Path: root})
				}
			}
			resp["path"] = ""
			resp["parent"] = nil
			resp["entries"] = entries
			writeJSONOK(w, resp)
			return
		}
		p = string(filepath.Separator)
	}

	if !filepath.IsAbs(p) {
		writeErr(w, http.StatusBadRequest, "path must be absolute")
		return
	}
	p = filepath.Clean(p)
	des, err := os.ReadDir(p)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "cannot read directory: "+err.Error())
		return
	}
	entries := []browseEntry{}
	looksLikeLibrary := false
	for _, de := range des {
		if !isDirEntry(de, p) {
			continue
		}
		if strings.EqualFold(de.Name(), "mangas") {
			looksLikeLibrary = true
		}
		if skipName(de.Name()) {
			continue
		}
		entries = append(entries, browseEntry{Name: de.Name(), Path: filepath.Join(p, de.Name())})
	}
	sort.Slice(entries, func(i, j int) bool { return NaturalLess(entries[i].Name, entries[j].Name) })

	var parent any
	if dir := filepath.Dir(p); dir != p {
		parent = dir
	} else if runtime.GOOS == "windows" {
		parent = "" // drive root: go back to the drive list
	}
	resp["path"] = p
	resp["parent"] = parent
	resp["entries"] = entries
	resp["looksLikeLibrary"] = looksLikeLibrary
	writeJSONOK(w, resp)
}

// ---------------------------------------------------------------------------
// Libraries
// ---------------------------------------------------------------------------

type adminLibrary struct {
	*Library
	Scan *ScanStatus `json:"scan"`
}

func (h *Handler) adminLib(l *Library) adminLibrary {
	return adminLibrary{Library: l, Scan: h.scanner.Status(l.ID)}
}

// AdminListLibraries lists libraries with their latest scan status.
func (h *Handler) AdminListLibraries(w http.ResponseWriter, r *http.Request) {
	libs, err := h.store.ListLibraries()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	out := make([]adminLibrary, 0, len(libs))
	for _, l := range libs {
		out = append(out, h.adminLib(l))
	}
	writeJSONOK(w, out)
}

func validateLibraryRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" || !filepath.IsAbs(root) {
		return "", errors.New("rootPath must be an absolute path")
	}
	root = filepath.Clean(root)
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		return "", errors.New("rootPath is not an accessible directory")
	}
	return root, nil
}

// AdminCreateLibrary registers a library and (by default) starts a scan.
func (h *Handler) AdminCreateLibrary(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string `json:"name"`
		RootPath string `json:"rootPath"`
		Scan     *bool  `json:"scan"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	root, err := validateLibraryRoot(in.RootPath)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	lib, err := h.store.CreateLibrary(in.Name, root)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	if in.Scan == nil || *in.Scan {
		h.scanner.ScanAsync(lib, "library created")
	}
	writeJSON(w, http.StatusCreated, h.adminLib(lib))
}

// AdminUpdateLibrary renames a library or moves its root (triggering a rescan).
func (h *Handler) AdminUpdateLibrary(w http.ResponseWriter, r *http.Request) {
	lib, err := h.store.GetLibrary(r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		writeErr(w, http.StatusNotFound, "library not found")
		return
	}
	if err != nil {
		h.internalErr(w, err)
		return
	}
	var in struct {
		Name     *string `json:"name"`
		RootPath *string `json:"rootPath"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	name, root := lib.Name, lib.RootPath
	rootChanged := false
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
		if name == "" {
			writeErr(w, http.StatusBadRequest, "name must not be empty")
			return
		}
	}
	if in.RootPath != nil {
		nr, err := validateLibraryRoot(*in.RootPath)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		rootChanged = nr != root
		root = nr
	}
	if err := h.store.UpdateLibrary(lib.ID, name, root); err != nil {
		h.internalErr(w, err)
		return
	}
	lib, _ = h.store.GetLibrary(lib.ID)
	if rootChanged {
		h.scanner.ScanAsync(lib, "root changed")
	}
	writeJSONOK(w, h.adminLib(lib))
}

// AdminDeleteLibrary removes a library from the catalog (files are untouched).
func (h *Handler) AdminDeleteLibrary(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if h.scanner.IsRunning(id) {
		writeErr(w, http.StatusConflict, "a scan is running for this library")
		return
	}
	ids, err := h.store.DeleteLibrary(id)
	if errors.Is(err, ErrNotFound) {
		writeErr(w, http.StatusNotFound, "library not found")
		return
	}
	if err != nil {
		h.internalErr(w, err)
		return
	}
	for _, sid := range ids {
		h.covers.Remove(sid)
	}
	writeJSONOK(w, map[string]bool{"ok": true})
}

// AdminScanLibrary starts a background scan (202) or reports one is running (409).
func (h *Handler) AdminScanLibrary(w http.ResponseWriter, r *http.Request) {
	lib, err := h.store.GetLibrary(r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		writeErr(w, http.StatusNotFound, "library not found")
		return
	}
	if err != nil {
		h.internalErr(w, err)
		return
	}
	if !h.scanner.ScanAsync(lib, "manual scan") {
		writeErr(w, http.StatusConflict, "a scan is already running for this library")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"started": true, "libraryId": lib.ID})
}

// AdminScanProgress returns the latest scan status of a library.
func (h *Handler) AdminScanProgress(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.store.GetLibrary(id); err != nil {
		writeErr(w, http.StatusNotFound, "library not found")
		return
	}
	st := h.scanner.Status(id)
	if st == nil {
		writeJSONOK(w, map[string]any{"libraryId": id, "running": false, "phase": "idle"})
		return
	}
	writeJSONOK(w, st)
}

// AdminListSources lists every source with its priority and series count.
func (h *Handler) AdminListSources(w http.ResponseWriter, r *http.Request) {
	list, err := h.store.ListAllSources()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSONOK(w, list)
}

// AdminListJobs returns recent scan jobs, with live progress for running ones.
func (h *Handler) AdminListJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.store.ListJobs(50)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	for _, j := range jobs {
		if j.Status != "running" {
			continue
		}
		if st := h.scanner.Status(j.LibraryID); st != nil && st.JobID == j.ID {
			j.Total, j.Done = st.SeriesTotal, st.SeriesDone
			if !st.Running {
				j.Status = st.Phase
			}
		}
	}
	writeJSONOK(w, jobs)
}

// AdminUpdateSeries patches series metadata (title, summary, authors, status,
// year, genres, tags, age rating, source priority). Writing text fields locks
// them against ComicInfo fills on rescan. Send "ageRating":"auto" to unlock age
// detection; "metaLocked":false to allow rescans to fill empty fields again.
// "sourcePriority":[] clears the per-series override (inherit global default).
func (h *Handler) AdminUpdateSeries(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in struct {
		Title          *string   `json:"title"`
		Summary        *string   `json:"summary"`
		Authors        *string   `json:"authors"`
		Status         *string   `json:"status"`
		Year           *int      `json:"year"`
		Genres         *[]string `json:"genres"`
		Tags           *[]string `json:"tags"`
		AgeRating      *string   `json:"ageRating"`
		MetaLocked     *bool     `json:"metaLocked"`
		SourcePriority *[]string `json:"sourcePriority"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Title == nil && in.Summary == nil && in.Authors == nil && in.Status == nil &&
		in.Year == nil && in.Genres == nil && in.Tags == nil && in.AgeRating == nil &&
		in.MetaLocked == nil && in.SourcePriority == nil {
		writeErr(w, http.StatusBadRequest, "no fields to update")
		return
	}
	err := h.store.UpdateSeriesMeta(id, SeriesMetaPatch{
		Title: in.Title, Summary: in.Summary, Authors: in.Authors, Status: in.Status,
		Year: in.Year, Genres: in.Genres, Tags: in.Tags, AgeRating: in.AgeRating,
		MetaLocked: in.MetaLocked, SourcePriority: in.SourcePriority,
	})
	if errors.Is(err, ErrNotFound) {
		writeErr(w, http.StatusNotFound, "series not found")
		return
	}
	if errors.Is(err, ErrBadRequest) {
		msg := err.Error()
		if i := strings.Index(msg, ": "); i >= 0 {
			msg = msg[i+2:]
		}
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSONOK(w, map[string]bool{"ok": true})
}

// AdminSetSeriesCover overrides a series cover. Accepts either:
//   - multipart form field "file" (image upload)
//   - JSON {"chapterId":"..."} to use a chapter's first page
//   - JSON {"auto":true} to unlock and regenerate from the lowest chapter
func (h *Handler) AdminSetSeriesCover(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var exists int
	if err := h.store.db.QueryRow(`SELECT 1 FROM series WHERE id = ?`, id).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "series not found")
		return
	} else if err != nil {
		h.internalErr(w, err)
		return
	}

	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/") {
		if err := r.ParseMultipartForm(12 << 20); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid multipart body")
			return
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			writeErr(w, http.StatusBadRequest, "missing file field")
			return
		}
		defer f.Close()
		img, err := imaging.Decode(io.LimitReader(f, maxCoverSourceBytes), imaging.AutoOrientation(true))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "could not decode image (use JPEG, PNG, WebP or GIF)")
			return
		}
		if err := h.covers.WriteImage(id, img); err != nil {
			h.internalErr(w, err)
			return
		}
		writeJSONOK(w, map[string]any{"ok": true, "coverLocked": true, "coverUrl": "/api/covers/" + id})
		return
	}

	var in struct {
		ChapterID string `json:"chapterId"`
		Auto      bool   `json:"auto"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	switch {
	case in.Auto:
		if err := h.covers.ResetAuto(id); err != nil {
			if errors.Is(err, ErrNotFound) {
				writeErr(w, http.StatusNotFound, "series not found")
				return
			}
			h.internalErr(w, err)
			return
		}
		writeJSONOK(w, map[string]any{"ok": true, "coverLocked": false, "coverUrl": "/api/covers/" + id})
	case strings.TrimSpace(in.ChapterID) != "":
		if err := h.covers.GenerateFromChapter(id, in.ChapterID); err != nil {
			if errors.Is(err, ErrNotFound) {
				writeErr(w, http.StatusNotFound, "chapter not found")
				return
			}
			if errors.Is(err, ErrForbidden) {
				writeErr(w, http.StatusBadRequest, "chapter does not belong to this series")
				return
			}
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSONOK(w, map[string]any{"ok": true, "coverLocked": true, "coverUrl": "/api/covers/" + id})
	default:
		writeErr(w, http.StatusBadRequest, "provide file, chapterId, or auto")
	}
}

// ---------------------------------------------------------------------------
// Users
// ---------------------------------------------------------------------------

// AdminListUsers lists accounts with their library whitelists.
func (h *Handler) AdminListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.store.ListUsers()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSONOK(w, users)
}

func (h *Handler) validateLibraryIDs(ids []string) error {
	for _, id := range ids {
		if _, err := h.store.GetLibrary(id); err != nil {
			return errors.New("unknown library id: " + id)
		}
	}
	return nil
}

// AdminCreateUser creates a local account.
func (h *Handler) AdminCreateUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username     string   `json:"username"`
		Password     string   `json:"password"`
		Email        string   `json:"email"`
		IsAdmin      bool     `json:"isAdmin"`
		MaxAgeRating string   `json:"maxAgeRating"`
		Libraries    []string `json:"libraries"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	in.Username = strings.TrimSpace(in.Username)
	if !usernameRe.MatchString(in.Username) {
		writeErr(w, http.StatusBadRequest, "username must be 1-64 chars of letters, digits and . _ @ + -")
		return
	}
	if len(in.Password) < 4 {
		writeErr(w, http.StatusBadRequest, "password must be at least 4 characters")
		return
	}
	rating := RatingTeen
	if in.IsAdmin {
		rating = RatingExplicit
	}
	if in.MaxAgeRating != "" {
		if rating = normalizeRating(in.MaxAgeRating); rating == "" {
			writeErr(w, http.StatusBadRequest, "maxAgeRating must be E, T, M or X")
			return
		}
	}
	if err := h.validateLibraryIDs(in.Libraries); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := h.store.GetUser(in.Username); err == nil {
		writeErr(w, http.StatusConflict, "username already exists")
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	u := &User{Username: in.Username, Email: strings.TrimSpace(in.Email), IsAdmin: in.IsAdmin, MaxAgeRating: rating}
	if err := h.store.CreateUser(u, hash); err != nil {
		h.internalErr(w, err)
		return
	}
	if len(in.Libraries) > 0 {
		if err := h.store.SetUserLibraries(in.Username, in.Libraries); err != nil {
			h.internalErr(w, err)
			return
		}
	}
	created, err := h.store.GetUser(in.Username)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// AdminUpdateUser updates password, email, admin flag, age limit and library access.
func (h *Handler) AdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	target, err := h.store.GetUser(username)
	if errors.Is(err, ErrNotFound) {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		h.internalErr(w, err)
		return
	}
	var in struct {
		Password     *string   `json:"password"`
		Email        *string   `json:"email"`
		IsAdmin      *bool     `json:"isAdmin"`
		MaxAgeRating *string   `json:"maxAgeRating"`
		Libraries    *[]string `json:"libraries"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	patch := UserPatch{}
	if in.Password != nil {
		if len(*in.Password) < 4 {
			writeErr(w, http.StatusBadRequest, "password must be at least 4 characters")
			return
		}
		hash, err := auth.HashPassword(*in.Password)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		patch.PasswordHash = &hash
	}
	if in.Email != nil {
		e := strings.TrimSpace(*in.Email)
		patch.Email = &e
	}
	if in.IsAdmin != nil {
		if !*in.IsAdmin && target.IsAdmin {
			if n, _ := h.store.CountAdmins(); n <= 1 {
				writeErr(w, http.StatusBadRequest, "cannot demote the last admin")
				return
			}
		}
		patch.IsAdmin = in.IsAdmin
	}
	if in.MaxAgeRating != nil {
		rating := normalizeRating(*in.MaxAgeRating)
		if rating == "" {
			writeErr(w, http.StatusBadRequest, "maxAgeRating must be E, T, M or X")
			return
		}
		patch.MaxAgeRating = &rating
	}
	if in.Libraries != nil {
		if err := h.validateLibraryIDs(*in.Libraries); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := h.store.UpdateUser(target.Username, patch); err != nil {
		h.internalErr(w, err)
		return
	}
	if in.Libraries != nil {
		if err := h.store.SetUserLibraries(target.Username, *in.Libraries); err != nil {
			h.internalErr(w, err)
			return
		}
	}
	updated, err := h.store.GetUser(target.Username)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSONOK(w, updated)
}

// AdminDeleteUser deletes an account (never yourself or the last admin).
func (h *Handler) AdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	if s := auth.FromContext(r.Context()); s != nil && strings.EqualFold(s.Username, username) {
		writeErr(w, http.StatusBadRequest, "you cannot delete your own account")
		return
	}
	target, err := h.store.GetUser(username)
	if errors.Is(err, ErrNotFound) {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		h.internalErr(w, err)
		return
	}
	if target.IsAdmin {
		if n, _ := h.store.CountAdmins(); n <= 1 {
			writeErr(w, http.StatusBadRequest, "cannot delete the last admin")
			return
		}
	}
	if err := h.store.DeleteUser(target.Username); err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSONOK(w, map[string]bool{"ok": true})
}

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

func (h *Handler) settingsPayload() (map[string]any, error) {
	prio, err := h.store.GetSetting(SettingSourcePriority, "")
	if err != nil {
		return nil, err
	}
	hoursStr, _ := h.store.GetSetting(SettingAutoScanHours, "0")
	hours, _ := strconv.Atoi(strings.TrimSpace(hoursStr))
	mode, _ := h.store.GetSetting(SettingDefaultReaderMode, "auto")
	sources, err := h.store.ListAllSources()
	if err != nil {
		return nil, err
	}
	return map[string]any{
		SettingSourcePriority:       prio,
		SettingAutoScanHours:        hours,
		SettingDefaultReaderMode:    mode,
		SettingRandomRefreshMinutes: h.randomRefreshMinutes(),
		"sources":                   sources,
	}, nil
}

// randomRefreshMinutes returns the configured home Random stability window.
func (h *Handler) randomRefreshMinutes() int {
	raw, _ := h.store.GetSetting(SettingRandomRefreshMinutes, "")
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || !ValidRandomRefreshMinutes[n] {
		return DefaultRandomRefreshMinutes
	}
	return n
}

// AdminGetSettings returns the admin-configurable settings.
func (h *Handler) AdminGetSettings(w http.ResponseWriter, r *http.Request) {
	p, err := h.settingsPayload()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSONOK(w, p)
}

// AdminUpdateSettings applies any of source_priority (CSV), auto_scan_hours,
// default_reader_mode (fallback when a series has no per-user mode yet), and
// random_refresh_minutes (home Random swimlane stability window).
// Changing the priority takes effect immediately for the merged chapter lists
// (no rescan needed).
func (h *Handler) AdminUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SourcePriority        *string `json:"source_priority"`
		AutoScanHours         *int    `json:"auto_scan_hours"`
		DefaultReaderMode     *string `json:"default_reader_mode"`
		RandomRefreshMinutes  *int    `json:"random_refresh_minutes"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.DefaultReaderMode != nil && !ValidReaderModes[*in.DefaultReaderMode] {
		writeErr(w, http.StatusBadRequest, "default_reader_mode must be one of auto, ltr, rtl, vertical, webtoon")
		return
	}
	if in.AutoScanHours != nil && (*in.AutoScanHours < 0 || *in.AutoScanHours > 24*365) {
		writeErr(w, http.StatusBadRequest, "auto_scan_hours must be between 0 and 8760")
		return
	}
	if in.RandomRefreshMinutes != nil && !ValidRandomRefreshMinutes[*in.RandomRefreshMinutes] {
		writeErr(w, http.StatusBadRequest, "random_refresh_minutes must be one of 30, 60, 360, 720, 1440")
		return
	}
	if in.SourcePriority != nil {
		if err := h.store.ApplySourcePriority(*in.SourcePriority); err != nil {
			h.internalErr(w, err)
			return
		}
	}
	if in.AutoScanHours != nil {
		var err error
		if h.auto != nil {
			err = h.auto.SetIntervalHours(*in.AutoScanHours)
		} else {
			err = h.store.SetSetting(SettingAutoScanHours, strconv.Itoa(*in.AutoScanHours))
		}
		if err != nil {
			h.internalErr(w, err)
			return
		}
	}
	if in.DefaultReaderMode != nil {
		if err := h.store.SetSetting(SettingDefaultReaderMode, *in.DefaultReaderMode); err != nil {
			h.internalErr(w, err)
			return
		}
	}
	if in.RandomRefreshMinutes != nil {
		if err := h.store.SetSetting(SettingRandomRefreshMinutes, strconv.Itoa(*in.RandomRefreshMinutes)); err != nil {
			h.internalErr(w, err)
			return
		}
	}
	p, err := h.settingsPayload()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSONOK(w, p)
}
