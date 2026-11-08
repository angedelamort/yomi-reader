package library

import "time"

// Setting keys stored in the settings table.
const (
	SettingSourcePriority       = "source_priority"        // CSV of source names, best first
	SettingAutoScanHours        = "auto_scan_hours"        // 0 disables periodic rescans
	SettingDefaultReaderMode    = "default_reader_mode"    // ltr | rtl | vertical | webtoon | auto
	SettingBootstrapDone        = "bootstrap_done"         // default library bootstrapped once
	SettingRandomRefreshMinutes = "random_refresh_minutes" // home Random swimlane stability window
)

// DefaultRandomRefreshMinutes is how long the home Random lane stays stable
// when the setting is unset or invalid.
const DefaultRandomRefreshMinutes = 360 // 6 hours

// ValidRandomRefreshMinutes lists allowed home Random refresh windows.
var ValidRandomRefreshMinutes = map[int]bool{
	30: true, 60: true, 360: true, 720: true, 1440: true,
}

// ValidReaderModes lists accepted values for default_reader_mode / per-series readerMode.
// "auto" is only valid as an admin default (resolved to webtoon by the client).
var ValidReaderModes = map[string]bool{"auto": true, "ltr": true, "rtl": true, "vertical": true, "webtoon": true}

// ValidSeriesReaderModes lists modes a user may pin on a series.
var ValidSeriesReaderModes = map[string]bool{"ltr": true, "rtl": true, "vertical": true, "webtoon": true}

// Library is a registered library root.
type Library struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	RootPath    string     `json:"rootPath"`
	CreatedAt   time.Time  `json:"createdAt"`
	LastScan    *time.Time `json:"lastScan"`
	SeriesCount int        `json:"seriesCount"`
}

// Source is a manga source folder (e.g. "MangaDex (EN)") inside a library.
type Source struct {
	ID          string `json:"id"`
	LibraryID   string `json:"libraryId"`
	Name        string `json:"name"`
	Priority    int    `json:"priority"`
	FSPath      string `json:"-"`
	SeriesCount int    `json:"seriesCount"`
}

// Series is a merged series (one logical title, possibly from several sources).
type Series struct {
	ID           string     `json:"id"`
	LibraryID    string     `json:"libraryId"`
	Title        string     `json:"title"`
	SortTitle    string     `json:"sortTitle"`
	Summary      string     `json:"summary"`
	Authors      string     `json:"authors"`
	Status       string     `json:"status"`
	Year         int        `json:"year"`
	AgeRating    string     `json:"ageRating"`
	AgeLocked    bool       `json:"ageLocked,omitempty"`
	Genres       []string   `json:"genres"`
	Tags         []string   `json:"tags"`
	CoverRel     string     `json:"-"`
	CoverLocked  bool       `json:"coverLocked,omitempty"`
	MetaLocked     bool       `json:"metaLocked,omitempty"`
	HasCover       bool       `json:"hasCover"`
	ChapterCount   int        `json:"chapterCount"`
	AddedAt        time.Time  `json:"addedAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	Sources        []string   `json:"sources"`
	SourcePriority []string   `json:"sourcePriority,omitempty"` // per-series override; empty = global default
	Favorite       bool       `json:"favorite"`
	LastReadAt     *time.Time `json:"lastReadAt,omitempty"`
	CoverURL       string     `json:"coverUrl"`

	NormalizedTitle string `json:"-"`
}

// Chapter formats.
const (
	FormatCBZ    = "cbz"
	FormatEPUB   = "epub"
	FormatPDF    = "pdf"
	FormatFolder = "folder"
)

// ChapterAlt describes a lower-priority copy of a merged chapter.
type ChapterAlt struct {
	ID         string `json:"id"`
	SourceID   string `json:"sourceId"`
	SourceName string `json:"sourceName"`
	Edition    string `json:"edition,omitempty"` // e.g. official, unofficial
	PageCount  int    `json:"pageCount"`
}

// Chapter is one readable unit stored in a single source.
// Number is -1 when it could not be determined from the file name.
type Chapter struct {
	ID         string       `json:"id"`
	SeriesID   string       `json:"seriesId"`
	SourceID   string       `json:"sourceId"`
	SourceName string       `json:"sourceName"`
	Number     float64      `json:"number"`
	Volume     *float64     `json:"volume,omitempty"`
	Title      string       `json:"title"`
	Edition    string       `json:"edition,omitempty"` // e.g. official, unofficial (from filename)
	FSPath     string       `json:"-"`
	Format     string       `json:"format"`
	PageCount  int          `json:"pageCount"`
	SizeBytes  int64        `json:"sizeBytes"`
	ReleasedAt *time.Time   `json:"releasedAt,omitempty"`
	ScannedAt  time.Time    `json:"scannedAt"`
	Alts       []ChapterAlt `json:"alternatives,omitempty"`

	Priority int   `json:"-"` // source priority (lower = preferred)
	MTime    int64 `json:"-"`
}

// ChapterRef is a chapter together with the library/series info needed for
// access checks and path confinement.
type ChapterRef struct {
	Chapter
	LibraryID       string
	LibraryRoot     string
	SeriesAgeRating string
}

// Progress is a user's reading position within a series.
type Progress struct {
	SeriesID      string    `json:"seriesId"`
	ChapterID     string    `json:"chapterId"`
	ChapterNumber float64   `json:"chapterNumber"`
	PageIndex     int       `json:"pageIndex"`
	ScrollRatio   float64   `json:"scrollRatio"`
	Completed     bool      `json:"completed"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// User is an application account.
type User struct {
	Username     string     `json:"username"`
	Email        string     `json:"email"`
	IsAdmin      bool       `json:"isAdmin"`
	MaxAgeRating string     `json:"maxAgeRating"`
	HasPassword  bool       `json:"hasPassword"`
	CreatedAt    time.Time  `json:"createdAt"`
	LastSeen     *time.Time `json:"lastSeen"`
	Libraries    []string   `json:"libraries"`

	PasswordHash string `json:"-"`
}

// Viewer carries the identity and restrictions applied to catalog queries.
type Viewer struct {
	Username  string
	IsAdmin   bool
	MaxRating string // age rating code; ignored for admins
}

// CanSee reports whether the viewer's age limit allows the rating. Library
// access is checked separately in SQL / CanAccessLibrary.
func (v Viewer) CanSeeRating(rating string) bool {
	return v.IsAdmin || ratingRank(rating) <= ratingRank(v.MaxRating)
}

// TagCount is a genre/tag with the number of visible series using it.
type TagCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// SeriesPage is a page of series results.
type SeriesPage struct {
	Items  []*Series `json:"items"`
	Total  int       `json:"total"`
	Offset int       `json:"offset"`
	Limit  int       `json:"limit"`
}

// Job is a recorded background job (library scan).
type Job struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	LibraryID  string     `json:"libraryId"`
	Label      string     `json:"label"`
	Status     string     `json:"status"` // running | done | error | interrupted
	Message    string     `json:"message"`
	Total      int        `json:"total"`
	Done       int        `json:"done"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}
