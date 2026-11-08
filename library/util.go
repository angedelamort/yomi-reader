package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// Sentinel errors shared across the package.
var (
	ErrNotFound   = errors.New("not found")
	ErrUnsafePath = errors.New("path escapes library root")
	ErrForbidden  = errors.New("forbidden")
	ErrBadRequest = errors.New("bad request")
)

// ---------------------------------------------------------------------------
// JSON helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONOK(w http.ResponseWriter, v any) { writeJSON(w, http.StatusOK, v) }

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// decodeJSON decodes a size-limited JSON body into v.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// IDs and time
// ---------------------------------------------------------------------------

func newID() string { return uuid.NewString() }

func nowUnix() int64 { return time.Now().Unix() }

func unixTime(sec int64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}

func unixTimePtr(sec int64) *time.Time {
	if sec <= 0 {
		return nil
	}
	t := time.Unix(sec, 0).UTC()
	return &t
}

// ---------------------------------------------------------------------------
// Natural sort
// ---------------------------------------------------------------------------

// NaturalLess reports whether a sorts before b using natural ordering
// ("page2" < "page10"), case-insensitively.
func NaturalLess(a, b string) bool { return naturalCompare(a, b) < 0 }

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func naturalCompare(a, b string) int {
	a, b = strings.ToLower(a), strings.ToLower(b)
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if isDigit(a[i]) && isDigit(b[j]) {
			si := i
			for i < len(a) && isDigit(a[i]) {
				i++
			}
			sj := j
			for j < len(b) && isDigit(b[j]) {
				j++
			}
			na := strings.TrimLeft(a[si:i], "0")
			nb := strings.TrimLeft(b[sj:j], "0")
			if len(na) != len(nb) {
				if len(na) < len(nb) {
					return -1
				}
				return 1
			}
			if na != nb {
				if na < nb {
					return -1
				}
				return 1
			}
			continue
		}
		if a[i] != b[j] {
			if a[i] < b[j] {
				return -1
			}
			return 1
		}
		i++
		j++
	}
	switch {
	case len(a)-i < len(b)-j:
		return -1
	case len(a)-i > len(b)-j:
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------
// Titles
// ---------------------------------------------------------------------------

// normalizeTitle builds the merge key for a series title: lowercase letters
// and digits separated by single spaces, apostrophes dropped, "&" -> "and".
func normalizeTitle(s string) string {
	var b strings.Builder
	lastSpace := true
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			lastSpace = false
		case r == '\'' || r == '’' || r == '`' || r == '‘':
			// dropped: "Hero's" == "Heros"
		case r == '&':
			if !lastSpace {
				b.WriteByte(' ')
			}
			b.WriteString("and ")
			lastSpace = true
		default:
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		out = strings.ToLower(strings.TrimSpace(s))
	}
	return out
}

// sortTitle drops leading articles so "The Boxer" sorts under B.
func sortTitle(title string) string {
	n := normalizeTitle(title)
	for _, art := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(n, art) && len(n) > len(art) {
			return n[len(art):]
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Chapter filename parsing
// ---------------------------------------------------------------------------

var (
	reChapter = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(?:chapters?|chap|ch|episodes?|ep)[\s._#-]*(\d+(?:\.\d+)?)`)
	reVolume  = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(?:volumes?|vol|v)[\s._#-]*(\d+(?:\.\d+)?)`)
	reHash    = regexp.MustCompile(`#\s*(\d+(?:\.\d+)?)`)
	reAnyNum  = regexp.MustCompile(`(\d{1,6}(?:\.\d+)?)`)
	// Leading edition token used by some aggregators (e.g. MangaFire):
	// "official_Ch. 1.cbz", "unofficial_Ch. 1.cbz".
	reEditionPrefix = regexp.MustCompile(`(?i)^([a-z][a-z0-9]*)[_.\-\s]+`)
)

// knownEditionRank maps filename edition tokens to preference (lower = better).
// Official wins when present; unofficial is last; unknown/other editions sit in
// between so they are used when official is missing.
var knownEditionRank = map[string]int{
	"official":   0,
	"unofficial": 2,
}

// editionRank returns the merge preference for an edition token (lower = better).
func editionRank(ed string) int {
	ed = strings.ToLower(strings.TrimSpace(ed))
	if ed == "" {
		return 1
	}
	if r, ok := knownEditionRank[ed]; ok {
		return r
	}
	return 1
}

// splitEditionTitle pulls a known leading edition token off a chapter title.
// Unknown prefixes (e.g. "MANGA Plus_#001") are left alone.
func splitEditionTitle(title string) (edition, cleaned string) {
	cleaned = strings.TrimSpace(title)
	m := reEditionPrefix.FindStringSubmatch(cleaned)
	if m == nil {
		return "", cleaned
	}
	tok := strings.ToLower(m[1])
	if _, ok := knownEditionRank[tok]; !ok {
		return "", cleaned
	}
	rest := strings.TrimSpace(cleaned[len(m[0]):])
	if rest == "" {
		return tok, cleaned
	}
	return tok, rest
}

// editionFromName returns the known edition token in a file/folder name, if any.
func editionFromName(name string) string {
	base := chapterBaseName(name, true)
	ed, _ := splitEditionTitle(base)
	return ed
}

// chapterExts are the file extensions recognised as chapter containers.
var chapterExts = map[string]string{
	".cbz":  FormatCBZ,
	".zip":  FormatCBZ,
	".epub": FormatEPUB,
	".pdf":  FormatPDF,
}

// unsupportedExts are recognised but not readable (counted by the scanner).
var unsupportedExts = map[string]bool{".cbr": true, ".rar": true, ".cb7": true, ".7z": true}

// chapterBaseName strips a known chapter extension and a leading "# ".
func chapterBaseName(name string, isFile bool) string {
	if isFile {
		if _, ok := chapterExts[strings.ToLower(filepath.Ext(name))]; ok {
			name = strings.TrimSuffix(name, filepath.Ext(name))
		}
	}
	name = strings.TrimSpace(name)
	name = strings.TrimSpace(strings.TrimPrefix(name, "#"))
	return name
}

// parseChapterName extracts the chapter number (-1 when unknown), the optional
// volume and a display title from a chapter file/folder name.
//
// Handles "Chapter 11.5", "Ch.006.1", "Vol.6 Ch.238", "#001", "MANGA Plus_#001 - Sword 1".
func parseChapterName(name string, isFile bool) (number float64, volume *float64, title string) {
	title = chapterBaseName(name, isFile)
	number = -1

	if m := reVolume.FindStringSubmatch(title); m != nil {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			volume = &v
		}
	}
	if m := reChapter.FindStringSubmatch(title); m != nil {
		if n, err := strconv.ParseFloat(m[1], 64); err == nil {
			return n, volume, title
		}
	}
	if m := reHash.FindStringSubmatch(title); m != nil {
		if n, err := strconv.ParseFloat(m[1], 64); err == nil {
			return n, volume, title
		}
	}
	if volume != nil {
		// Volume-only release: treat the volume number as the chapter number.
		return *volume, volume, title
	}
	if m := reAnyNum.FindStringSubmatch(title); m != nil {
		if n, err := strconv.ParseFloat(m[1], 64); err == nil {
			return n, volume, title
		}
	}
	return -1, volume, title
}

// ---------------------------------------------------------------------------
// CSV lists (genres, tags)
// ---------------------------------------------------------------------------

// splitCSV splits a comma/semicolon separated list into trimmed unique items.
func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return []string{}
	}
	seen := map[string]bool{}
	out := []string{}
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' }) {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		k := strings.ToLower(p)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, p)
	}
	return out
}

// joinCSV is the inverse of splitCSV; items never contain commas.
func joinCSV(items []string) string {
	clean := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, it := range items {
		it = strings.TrimSpace(strings.NewReplacer(",", " ", ";", " ").Replace(it))
		if it == "" || seen[strings.ToLower(it)] {
			continue
		}
		seen[strings.ToLower(it)] = true
		clean = append(clean, it)
	}
	return strings.Join(clean, ",")
}

// escapeLike escapes %, _ and \ for use with LIKE ... ESCAPE '\'.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// ---------------------------------------------------------------------------
// Age ratings
// ---------------------------------------------------------------------------

// Age rating codes, ordered from least to most restricted.
const (
	RatingEveryone = "E"
	RatingTeen     = "T"
	RatingMature   = "M"
	RatingExplicit = "X"
)

func ratingRank(code string) int {
	switch code {
	case RatingEveryone:
		return 0
	case RatingTeen:
		return 1
	case RatingMature:
		return 2
	default:
		return 3
	}
}

// normalizeRating maps user/API input ("Teen", "mature", "x", ...) to a code.
// It returns "" when the value is not recognised.
func normalizeRating(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "e", "everyone", "all", "kids", "children":
		return RatingEveryone
	case "t", "teen", "teens":
		return RatingTeen
	case "m", "mature":
		return RatingMature
	case "x", "explicit", "adult", "adults":
		return RatingExplicit
	}
	return ""
}

// ratingFromComicInfo maps ComicInfo.xml AgeRating strings to a code ("" if unknown).
func ratingFromComicInfo(s string) string {
	l := strings.ToLower(strings.TrimSpace(s))
	switch {
	case l == "":
		return ""
	case strings.Contains(l, "adults only"), strings.Contains(l, "x18"), strings.Contains(l, "r18"), strings.Contains(l, "18+"):
		return RatingExplicit
	case strings.Contains(l, "mature"), strings.Contains(l, "ma15"), strings.Contains(l, "17+"), l == "m":
		return RatingMature
	case strings.Contains(l, "teen"), strings.Contains(l, "pg-13"), l == "t":
		return RatingTeen
	case strings.Contains(l, "everyone"), strings.Contains(l, "early childhood"), strings.Contains(l, "kids"), l == "g", l == "pg":
		return RatingEveryone
	}
	return ""
}

func maxRating(a, b string) string {
	if ratingRank(b) > ratingRank(a) {
		return b
	}
	return a
}

// ---------------------------------------------------------------------------
// Paths
// ---------------------------------------------------------------------------

// underRoot reports whether p is root itself or located inside root, using
// cleaned absolute paths (case-insensitive on Windows via filepath.Rel).
func underRoot(root, p string) bool {
	r, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	q, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(r, q)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// ResolveUnderRoot returns the cleaned absolute form of p, or ErrUnsafePath if
// it is not located under root.
func ResolveUnderRoot(root, p string) (string, error) {
	if p == "" || root == "" {
		return "", ErrUnsafePath
	}
	abs, err := filepath.Abs(p)
	if err != nil || !underRoot(root, abs) {
		return "", ErrUnsafePath
	}
	return abs, nil
}

// ---------------------------------------------------------------------------
// Misc
// ---------------------------------------------------------------------------

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func queryInt(r *http.Request, key string, def int) int {
	if v := r.URL.Query().Get(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func humanErr(prefix string, err error) string { return fmt.Sprintf("%s: %v", prefix, err) }
