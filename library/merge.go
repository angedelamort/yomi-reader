package library

import (
	"math"
	"path/filepath"
	"sort"
	"strings"
)

// numKey converts a chapter number to a stable map key (3 decimal places).
func numKey(n float64) int64 { return int64(math.Round(n * 1000)) }

// ApplySourcePriorityToChapters remaps each chapter's Priority from the given
// ordered source-name list (best first). An empty list leaves priorities alone
// (global sources.priority ranks stay in effect).
func ApplySourcePriorityToChapters(chs []*Chapter, list []string) {
	if len(list) == 0 {
		return
	}
	for _, c := range chs {
		if c != nil {
			c.Priority = priorityFor(list, c.SourceName)
		}
	}
}

// ensureEdition fills Chapter.Edition from the title or file name when empty.
func ensureEdition(c *Chapter) {
	if c == nil || c.Edition != "" {
		return
	}
	if ed, _ := splitEditionTitle(c.Title); ed != "" {
		c.Edition = ed
		return
	}
	if c.FSPath != "" {
		c.Edition = editionFromName(filepath.Base(c.FSPath))
	}
}

// chapterAlt builds an alternatives entry from a losing chapter copy.
func chapterAlt(c *Chapter) ChapterAlt {
	ensureEdition(c)
	return ChapterAlt{ID: c.ID, SourceID: c.SourceID, SourceName: c.SourceName, Edition: c.Edition, PageCount: c.PageCount}
}

// betterChapter reports whether a should be preferred over b: best source
// priority first, then preferred edition (official > other > unofficial), then
// more pages (a truncated copy loses), then name/path for determinism.
func betterChapter(a, b *Chapter) bool {
	ensureEdition(a)
	ensureEdition(b)
	if a.Priority != b.Priority {
		return a.Priority < b.Priority
	}
	if ar, br := editionRank(a.Edition), editionRank(b.Edition); ar != br {
		return ar < br
	}
	if a.PageCount != b.PageCount {
		return a.PageCount > b.PageCount
	}
	if an, bn := strings.ToLower(a.SourceName), strings.ToLower(b.SourceName); an != bn {
		return an < bn
	}
	return a.FSPath < b.FSPath
}

// MergeChapters collapses chapters from several sources into one list: for
// each chapter number the copy from the best-priority source is kept and the
// others are attached as Alts. Chapters without a parsed number (-1) are never
// merged and sort last in natural title order.
func MergeChapters(all []*Chapter) []*Chapter {
	byNum := map[int64][]*Chapter{}
	var unnumbered []*Chapter
	for _, c := range all {
		ensureEdition(c)
		if c.Number < 0 {
			unnumbered = append(unnumbered, c)
			continue
		}
		k := numKey(c.Number)
		byNum[k] = append(byNum[k], c)
	}

	out := make([]*Chapter, 0, len(byNum)+len(unnumbered))
	for _, group := range byNum {
		sort.Slice(group, func(i, j int) bool { return betterChapter(group[i], group[j]) })
		best := *group[0]
		best.Alts = nil
		for _, o := range group[1:] {
			if best.Volume == nil && o.Volume != nil {
				v := *o.Volume // inherit volume info from another source's copy
				best.Volume = &v
			}
			best.Alts = append(best.Alts, chapterAlt(o))
		}
		out = append(out, &best)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })

	sort.Slice(unnumbered, func(i, j int) bool {
		if c := naturalCompare(unnumbered[i].Title, unnumbered[j].Title); c != 0 {
			return c < 0
		}
		return betterChapter(unnumbered[i], unnumbered[j])
	})
	for _, c := range unnumbered {
		cc := *c
		cc.Alts = nil
		out = append(out, &cc)
	}
	return out
}

// SortChapters orders chapters by number (unnumbered last) then source
// priority; used for the allSources listing.
func SortChapters(chs []*Chapter) {
	sort.SliceStable(chs, func(i, j int) bool {
		a, b := chs[i], chs[j]
		if (a.Number < 0) != (b.Number < 0) {
			return a.Number >= 0
		}
		if a.Number != b.Number {
			return a.Number < b.Number
		}
		if a.Number < 0 {
			if c := naturalCompare(a.Title, b.Title); c != 0 {
				return c < 0
			}
		}
		return betterChapter(a, b)
	})
}

// VolumeGroup is a set of merged chapters belonging to one volume. Volume is
// nil for chapters without volume information.
type VolumeGroup struct {
	Volume   *float64   `json:"volume"`
	Chapters []*Chapter `json:"chapters"`
}

// GroupByVolume groups already merged chapters by volume (ascending, with the
// volume-less group last). Chapter order inside a group is preserved.
func GroupByVolume(merged []*Chapter) []VolumeGroup {
	groups := map[int64]*VolumeGroup{}
	var keys []int64
	var none *VolumeGroup
	for _, c := range merged {
		if c.Volume == nil {
			if none == nil {
				none = &VolumeGroup{}
			}
			none.Chapters = append(none.Chapters, c)
			continue
		}
		k := numKey(*c.Volume)
		g, ok := groups[k]
		if !ok {
			v := *c.Volume
			g = &VolumeGroup{Volume: &v}
			groups[k] = g
			keys = append(keys, k)
		}
		g.Chapters = append(g.Chapters, c)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	out := make([]VolumeGroup, 0, len(keys)+1)
	for _, k := range keys {
		out = append(out, *groups[k])
	}
	if none != nil {
		out = append(out, *none)
	}
	return out
}

// locateChapter finds the merged entry corresponding to chapter id (the entry
// itself, one of its alternatives, or — as a last resort — the same number).
// It returns the index in merged, or -1.
func locateChapter(merged []*Chapter, id string, number float64) int {
	for i, c := range merged {
		if c.ID == id {
			return i
		}
		for _, a := range c.Alts {
			if a.ID == id {
				return i
			}
		}
	}
	if number >= 0 {
		k := numKey(number)
		for i, c := range merged {
			if c.Number >= 0 && numKey(c.Number) == k {
				return i
			}
		}
	}
	return -1
}
