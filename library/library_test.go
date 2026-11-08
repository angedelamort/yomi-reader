package library

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestParseChapterName(t *testing.T) {
	cases := []struct {
		name   string
		file   bool
		number float64
		volume float64 // -1 when none
	}{
		{"Chapter 11.5.cbz", true, 11.5, -1},
		{"Ch.006.1.cbz", true, 6.1, -1},
		{"Vol.6 Ch.238.cbz", true, 238, 6},
		{"# Chapter 1.cbz", true, 1, -1},
		{"MANGA Plus_#001 - Sword 1.cbz", true, 1, -1},
		{"Ch.001 - Death~ and Reincarnation", false, 1, -1},
		{"Chapter 100.cbz", true, 100, -1},
		{"Vol.3.cbz", true, 3, 3},
		{"001.cbz", true, 1, -1},
		{"Prologue.cbz", true, -1, -1},
	}
	for _, c := range cases {
		n, v, _ := parseChapterName(c.name, c.file)
		if n != c.number {
			t.Errorf("%q: number = %v, want %v", c.name, n, c.number)
		}
		if (v == nil) != (c.volume < 0) || (v != nil && *v != c.volume) {
			t.Errorf("%q: volume = %v, want %v", c.name, v, c.volume)
		}
	}
}

func TestNaturalSort(t *testing.T) {
	in := []string{"page10.jpg", "page2.jpg", "Page1.jpg", "page01a.jpg", "page003.jpg"}
	sort.Slice(in, func(i, j int) bool { return NaturalLess(in[i], in[j]) })
	want := []string{"Page1.jpg", "page01a.jpg", "page2.jpg", "page003.jpg", "page10.jpg"}
	if !reflect.DeepEqual(in, want) {
		t.Errorf("got %v, want %v", in, want)
	}
}

func TestNormalizeTitle(t *testing.T) {
	if a, b := normalizeTitle("Hero's Return!"), normalizeTitle("heros  return"); a != b {
		t.Errorf("%q != %q", a, b)
	}
	if a, b := normalizeTitle("Test Series"), normalizeTitle("Test Series!"); a != b {
		t.Errorf("%q != %q", a, b)
	}
	if normalizeTitle("Tom & Jerry") != "tom and jerry" {
		t.Errorf("ampersand: %q", normalizeTitle("Tom & Jerry"))
	}
}

func TestAgeRating(t *testing.T) {
	if r := inferAgeRating([]string{"Hentai3z.CC (EN)"}, nil, nil, 0, nil); r != RatingExplicit {
		t.Errorf("hentai source: %s", r)
	}
	if r := inferAgeRating([]string{"MangaDex (EN)"}, []string{"Action", "Seinen"}, nil, 0, nil); r != RatingMature {
		t.Errorf("seinen: %s", r)
	}
	if r := inferAgeRating([]string{"X"}, []string{"Smut"}, nil, 0, nil); r != RatingExplicit {
		t.Errorf("smut: %s", r)
	}
	if r := inferAgeRating([]string{"X"}, []string{"Action"}, nil, 0.9, nil); r != RatingExplicit {
		t.Errorf("# prefix: %s", r)
	}
	if r := inferAgeRating([]string{"X"}, []string{"Comedy"}, nil, 0, nil); r != RatingTeen {
		t.Errorf("default: %s", r)
	}
	if ratingFromComicInfo("Adults Only 18+") != RatingExplicit || ratingFromComicInfo("Mature 17+") != RatingMature ||
		ratingFromComicInfo("Everyone 10+") != RatingEveryone {
		t.Error("comicinfo rating mapping")
	}
}

func TestMergeChapters(t *testing.T) {
	ch := func(id, src string, prio int, n float64, pages int) *Chapter {
		return &Chapter{ID: id, SourceID: src, SourceName: src, Priority: prio, Number: n, PageCount: pages, Title: id}
	}
	all := []*Chapter{
		ch("a1", "A", 1, 1, 10), ch("a2", "A", 1, 2, 10),
		ch("b2", "B", 0, 2, 12), ch("b3", "B", 0, 3, 12),
		ch("u1", "A", 1, -1, 5), ch("u2", "B", 0, -1, 5),
	}
	m := MergeChapters(all)
	var ids []string
	for _, c := range m {
		ids = append(ids, c.ID)
	}
	want := []string{"a1", "b2", "b3", "u1", "u2"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("merged ids = %v, want %v", ids, want)
	}
	if len(m[1].Alts) != 1 || m[1].Alts[0].ID != "a2" {
		t.Errorf("alts of ch 2 = %+v", m[1].Alts)
	}
}

func TestApplySourcePriorityToChapters(t *testing.T) {
	ch := func(id, src string, prio int, n float64) *Chapter {
		return &Chapter{ID: id, SourceID: src, SourceName: src + " (EN)", Priority: prio, Number: n, PageCount: 10}
	}
	all := []*Chapter{ch("a1", "A", 0, 1), ch("b1", "B", 1, 1)}
	ApplySourcePriorityToChapters(all, []string{"B", "A"})
	m := MergeChapters(all)
	if m[0].ID != "b1" {
		t.Fatalf("expected B to win after series override, got %s", m[0].ID)
	}
	ApplySourcePriorityToChapters(all, nil) // empty list must not wipe existing ranks
	if all[0].Priority != 1 || all[1].Priority != 0 {
		t.Fatalf("empty list should leave priorities alone, got %d/%d", all[0].Priority, all[1].Priority)
	}
}

func TestUnderRoot(t *testing.T) {
	root := t.TempDir()
	if !underRoot(root, filepath.Join(root, "a", "b.cbz")) {
		t.Error("child should be under root")
	}
	if underRoot(root, filepath.Join(root, "..", "x")) {
		t.Error("parent escape must be rejected")
	}
	if _, err := ResolveUnderRoot(root, filepath.Join(root, "a", "..", "..", "etc")); err == nil {
		t.Error("traversal must be rejected")
	}
}

func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	// Cached archive handles keep files open; release them so TempDir cleanup
	// works on Windows.
	t.Cleanup(CloseArchives)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(body))
	}
	zw.Close()
	f.Close()
}

func TestCBZPages(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.cbz")
	writeZip(t, p, map[string]string{
		"10.jpg": "j10", "2.jpg": "j2", "1.png": "p1", "ComicInfo.xml": "<ComicInfo><Title>T</Title></ComicInfo>",
		"__MACOSX/1.jpg": "x", ".hidden.jpg": "x",
	})
	names, err := ListPages(p, FormatCBZ)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"1.png", "2.jpg", "10.jpg"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("pages = %v, want %v", names, want)
	}
	rc, ct, size, err := OpenPage(p, FormatCBZ, 1)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	rc.Close()
	if string(b) != "j2" || ct != "image/jpeg" || size != 2 {
		t.Errorf("page 1 = %q %s %d", b, ct, size)
	}
	if _, _, _, err := OpenPage(p, FormatCBZ, 3); err != ErrPageOutOfRange {
		t.Errorf("want ErrPageOutOfRange, got %v", err)
	}
	pages, ci, err := InspectChapter(p, FormatCBZ)
	if err != nil || pages != 3 || ci == nil {
		t.Errorf("inspect = %d %v %v", pages, ci != nil, err)
	}
}

func TestEPUBSpineOrder(t *testing.T) {
	p := filepath.Join(t.TempDir(), "b.epub")
	writeZip(t, p, map[string]string{
		"META-INF/container.xml": `<container><rootfiles><rootfile full-path="OEBPS/content.opf"/></rootfiles></container>`,
		"OEBPS/content.opf": `<package><manifest>
			<item id="p1" href="text/p1.xhtml" media-type="application/xhtml+xml"/>
			<item id="p2" href="text/p2.xhtml" media-type="application/xhtml+xml"/>
			<item id="i1" href="images/a.jpg" media-type="image/jpeg"/>
			<item id="i2" href="images/b.jpg" media-type="image/jpeg"/></manifest>
			<spine><itemref idref="p2"/><itemref idref="p1"/></spine></package>`,
		"OEBPS/text/p1.xhtml": `<html><body><img src="../images/a.jpg"/></body></html>`,
		"OEBPS/text/p2.xhtml": `<html><body><svg><image xlink:href="../images/b.jpg"/></svg></body></html>`,
		"OEBPS/images/a.jpg":  "A",
		"OEBPS/images/b.jpg":  "B",
	})
	names, err := ListPages(p, FormatEPUB)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"OEBPS/images/b.jpg", "OEBPS/images/a.jpg"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("epub pages = %v, want %v", names, want)
	}
}

func TestFolderAndPDF(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"2.jpg", "10.jpg", "notes.txt"} {
		_ = os.WriteFile(filepath.Join(dir, n), []byte(n), 0o644)
	}
	names, err := ListPages(dir, FormatFolder)
	if err != nil || !reflect.DeepEqual(names, []string{"2.jpg", "10.jpg"}) {
		t.Fatalf("folder pages = %v, %v", names, err)
	}
	pdf := filepath.Join(dir, "x.pdf")
	_ = os.WriteFile(pdf, []byte("%PDF"), 0o644)
	names, err = ListPages(pdf, FormatPDF)
	if err != nil || !reflect.DeepEqual(names, []string{"document.pdf"}) {
		t.Fatalf("pdf pages = %v, %v", names, err)
	}
	rc, ct, _, err := OpenPage(pdf, FormatPDF, 0)
	if err != nil || ct != "application/pdf" {
		t.Fatalf("pdf open: %v %s", err, ct)
	}
	rc.Close()
}
