package library

import (
	"bytes"
	"encoding/xml"
	"io"
	"strconv"
	"strings"
	"time"
)

// ComicInfo mirrors the subset of ComicInfo.xml (Anansi project schema) that
// Yomi Reader uses. Unknown elements, including Tachiyomi extensions, are ignored.
type ComicInfo struct {
	Title        string `xml:"Title"`
	Series       string `xml:"Series"`
	Number       string `xml:"Number"`
	Volume       string `xml:"Volume"`
	Summary      string `xml:"Summary"`
	Writer       string `xml:"Writer"`
	Penciller    string `xml:"Penciller"`
	Genre        string `xml:"Genre"`
	Tags         string `xml:"Tags"`
	AgeRating    string `xml:"AgeRating"`
	Year         string `xml:"Year"`
	Month        string `xml:"Month"`
	Day          string `xml:"Day"`
	Status       string `xml:"Status"`
	TachiStatus  string `xml:"PublishingStatusTachiyomi"`
	LanguageISO  string `xml:"LanguageISO"`
	Manga        string `xml:"Manga"`
	StoryArcName string `xml:"StoryArc"`
}

// parseComicInfo decodes ComicInfo.xml bytes (tolerating BOMs and odd charsets).
func parseComicInfo(data []byte) (*ComicInfo, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	dec.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	var ci ComicInfo
	if err := dec.Decode(&ci); err != nil {
		return nil, err
	}
	return &ci, nil
}

// number returns the chapter number declared in the XML, or -1.
func (c *ComicInfo) number() float64 {
	if n, err := strconv.ParseFloat(strings.TrimSpace(c.Number), 64); err == nil && n >= 0 {
		return n
	}
	return -1
}

// date returns the publication date when a Year is present.
func (c *ComicInfo) date() time.Time {
	y, err := strconv.Atoi(strings.TrimSpace(c.Year))
	if err != nil || y < 1900 || y > 2200 {
		return time.Time{}
	}
	m, _ := strconv.Atoi(strings.TrimSpace(c.Month))
	d, _ := strconv.Atoi(strings.TrimSpace(c.Day))
	if m < 1 || m > 12 {
		m = 1
	}
	if d < 1 || d > 31 {
		d = 1
	}
	return time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
}

// seriesMeta is the series-level metadata distilled from ComicInfo files.
type seriesMeta struct {
	Summary string
	Authors string
	Status  string
	Year    int
	Genres  []string
	Tags    []string
	Ratings []string // age rating codes found
}

// mergeComicInfo folds several ComicInfo documents into one seriesMeta,
// the first non-empty value of each field winning.
func mergeComicInfo(infos []*ComicInfo) seriesMeta {
	var m seriesMeta
	for _, ci := range infos {
		if m.Summary == "" {
			m.Summary = strings.TrimSpace(ci.Summary)
		}
		if m.Authors == "" {
			var names []string
			names = append(names, splitCSV(ci.Writer)...)
			names = append(names, splitCSV(ci.Penciller)...)
			m.Authors = joinCSVDisplay(names)
		}
		if m.Status == "" {
			m.Status = strings.TrimSpace(ci.TachiStatus)
			if m.Status == "" {
				m.Status = strings.TrimSpace(ci.Status)
			}
		}
		// Tachiyomi/Suwayomi write the chapter upload date in Year, which says
		// nothing about the series, so it is ignored for those files.
		if m.Year == 0 && strings.TrimSpace(ci.TachiStatus) == "" {
			if y, err := strconv.Atoi(strings.TrimSpace(ci.Year)); err == nil && y > 1900 && y < 2200 {
				m.Year = y
			}
		}
		if len(m.Genres) == 0 {
			m.Genres = splitCSV(ci.Genre)
		}
		if len(m.Tags) == 0 {
			m.Tags = splitCSV(ci.Tags)
		}
		if r := ratingFromComicInfo(ci.AgeRating); r != "" {
			m.Ratings = append(m.Ratings, r)
		}
	}
	return m
}

// joinCSVDisplay joins names for display ("A, B").
func joinCSVDisplay(names []string) string {
	seen := map[string]bool{}
	var out []string
	for _, n := range names {
		k := strings.ToLower(n)
		if n == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, n)
	}
	return strings.Join(out, ", ")
}

// inferAgeRating derives a series age rating from its sources, genres, tags,
// file naming and ComicInfo ratings. It is deliberately conservative: the
// result is the strictest rating suggested by any signal.
func inferAgeRating(sourceNames []string, genres, tags []string, hashPrefixRatio float64, ciRatings []string) string {
	rating := RatingTeen

	all := make([]string, 0, len(genres)+len(tags))
	for _, g := range append(append([]string{}, genres...), tags...) {
		all = append(all, strings.ToLower(strings.TrimSpace(g)))
	}
	has := func(words ...string) bool {
		for _, a := range all {
			for _, w := range words {
				if a == w {
					return true
				}
			}
		}
		return false
	}

	if has("kids", "children", "childrens", "children's") {
		rating = RatingEveryone
	}
	if has("ecchi", "seinen", "mature", "gore", "josei", "sexual violence") {
		rating = maxRating(rating, RatingMature)
	}
	if has("adult", "adults", "smut", "hentai", "pornographic", "erotica", "explicit", "nsfw", "18+", "r18", "r-18") {
		rating = maxRating(rating, RatingExplicit)
	}
	for _, sn := range sourceNames {
		l := strings.ToLower(sn)
		for _, kw := range []string{"hentai", "porn", "nsfw", "xxx", "18+"} {
			if strings.Contains(l, kw) {
				rating = maxRating(rating, RatingExplicit)
			}
		}
	}
	// Files named "# Chapter N" are typical of adult sources.
	if hashPrefixRatio >= 0.5 {
		rating = maxRating(rating, RatingExplicit)
	}
	for _, r := range ciRatings {
		rating = maxRating(rating, r)
	}
	return rating
}
