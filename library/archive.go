package library

import (
	"archive/zip"
	"container/list"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrPageOutOfRange is returned when a page index does not exist.
var ErrPageOutOfRange = errors.New("page out of range")

// ErrNoPages is returned when a chapter container holds no readable images.
var ErrNoPages = errors.New("no image pages found")

// pdfPageName is the single pseudo-page returned for PDF chapters.
const pdfPageName = "document.pdf"

var imageTypes = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".jfif": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
	".gif":  "image/gif",
	".avif": "image/avif",
	".bmp":  "image/bmp",
	".tif":  "image/tiff",
	".tiff": "image/tiff",
}

func isImageName(name string) bool {
	_, ok := imageTypes[strings.ToLower(path.Ext(name))]
	return ok
}

func imageContentType(name string) string {
	if t, ok := imageTypes[strings.ToLower(path.Ext(name))]; ok {
		return t
	}
	return "application/octet-stream"
}

// usablePageName filters hidden files and macOS resource forks inside archives.
func usablePageName(name string) bool {
	if strings.Contains(name, "__MACOSX/") {
		return false
	}
	base := path.Base(name)
	return !strings.HasPrefix(base, ".") && isImageName(base)
}

func normZipName(n string) string { return strings.ReplaceAll(n, `\`, "/") }

// FormatFromPath guesses a chapter format from a path ("" if unsupported).
func FormatFromPath(p string, isDir bool) string {
	if isDir {
		return FormatFolder
	}
	return chapterExts[strings.ToLower(filepath.Ext(p))]
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// ListPages returns the page names of a chapter in natural reading order. PDF
// chapters yield a single pseudo-page named "document.pdf".
func ListPages(p, format string) ([]string, error) {
	switch format {
	case FormatPDF:
		if _, err := os.Stat(p); err != nil {
			return nil, err
		}
		return []string{pdfPageName}, nil
	case FormatFolder:
		return listFolderImages(p)
	case FormatCBZ, FormatEPUB:
		e, err := archives.acquire(p, format)
		if err != nil {
			return nil, err
		}
		defer archives.release(e)
		names := make([]string, len(e.pages))
		for i, pg := range e.pages {
			names[i] = pg.name
		}
		return names, nil
	}
	return nil, fmt.Errorf("unsupported format %q", format)
}

// OpenPage opens page index of a chapter and returns the stream, its content
// type and size (-1 if unknown). The caller must Close the reader.
func OpenPage(p, format string, index int) (io.ReadCloser, string, int64, error) {
	if index < 0 {
		return nil, "", 0, ErrPageOutOfRange
	}
	switch format {
	case FormatPDF:
		if index != 0 {
			return nil, "", 0, ErrPageOutOfRange
		}
		f, err := os.Open(p)
		if err != nil {
			return nil, "", 0, err
		}
		st, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, "", 0, err
		}
		return f, "application/pdf", st.Size(), nil

	case FormatFolder:
		names, err := listFolderImages(p)
		if err != nil {
			return nil, "", 0, err
		}
		if index >= len(names) {
			return nil, "", 0, ErrPageOutOfRange
		}
		f, err := os.Open(filepath.Join(p, names[index]))
		if err != nil {
			return nil, "", 0, err
		}
		st, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, "", 0, err
		}
		return f, imageContentType(names[index]), st.Size(), nil

	case FormatCBZ, FormatEPUB:
		e, err := archives.acquire(p, format)
		if err != nil {
			return nil, "", 0, err
		}
		if index >= len(e.pages) {
			archives.release(e)
			return nil, "", 0, ErrPageOutOfRange
		}
		pg := e.pages[index]
		rc, err := pg.file.Open()
		if err != nil {
			archives.release(e)
			return nil, "", 0, err
		}
		return &zipPageReader{ReadCloser: rc, entry: e}, imageContentType(pg.name), int64(pg.file.UncompressedSize64), nil
	}
	return nil, "", 0, fmt.Errorf("unsupported format %q", format)
}

// InspectChapter counts the pages of a chapter and returns the raw
// ComicInfo.xml (nil when absent). It bypasses the reader cache.
func InspectChapter(p, format string) (pages int, comicInfo []byte, err error) {
	switch format {
	case FormatPDF:
		if _, err := os.Stat(p); err != nil {
			return 0, nil, err
		}
		return 1, nil, nil
	case FormatFolder:
		names, err := listFolderImages(p)
		if err != nil {
			return 0, nil, err
		}
		var ci []byte
		if f, err := os.Open(filepath.Join(p, "ComicInfo.xml")); err == nil {
			ci, _ = io.ReadAll(io.LimitReader(f, 1<<20))
			f.Close()
		}
		return len(names), ci, nil
	case FormatCBZ, FormatEPUB:
		zr, err := zip.OpenReader(p)
		if err != nil {
			return 0, nil, err
		}
		defer zr.Close()
		pgs := buildZipPages(&zr.Reader, format)
		var ci []byte
		if format == FormatCBZ {
			for _, f := range zr.File {
				if strings.EqualFold(path.Base(normZipName(f.Name)), "comicinfo.xml") && !f.FileInfo().IsDir() {
					ci, _ = readZipFile(f, 1<<20)
					break
				}
			}
		}
		return len(pgs), ci, nil
	}
	return 0, nil, fmt.Errorf("unsupported format %q", format)
}

// ---------------------------------------------------------------------------
// Folders
// ---------------------------------------------------------------------------

func listFolderImages(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasPrefix(n, ".") || !isImageName(n) {
			continue
		}
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return NaturalLess(names[i], names[j]) })
	return names, nil
}

// ---------------------------------------------------------------------------
// Zip-based formats (CBZ / ZIP / EPUB)
// ---------------------------------------------------------------------------

type pageEntry struct {
	name string
	file *zip.File
}

func buildZipPages(zr *zip.Reader, format string) []pageEntry {
	if format == FormatEPUB {
		if pgs := epubPages(zr); len(pgs) > 0 {
			return pgs
		}
	}
	return allZipImages(zr)
}

func allZipImages(zr *zip.Reader) []pageEntry {
	var pgs []pageEntry
	for _, f := range zr.File {
		name := normZipName(f.Name)
		if f.FileInfo().IsDir() || !usablePageName(name) {
			continue
		}
		pgs = append(pgs, pageEntry{name: name, file: f})
	}
	sort.SliceStable(pgs, func(i, j int) bool { return NaturalLess(pgs[i].name, pgs[j].name) })
	return pgs
}

func readZipFile(f *zip.File, limit int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, limit))
}

// --- EPUB ------------------------------------------------------------------

type opfPackage struct {
	Manifest []struct {
		ID        string `xml:"id,attr"`
		Href      string `xml:"href,attr"`
		MediaType string `xml:"media-type,attr"`
	} `xml:"manifest>item"`
	Spine []struct {
		IDRef string `xml:"idref,attr"`
	} `xml:"spine>itemref"`
}

var (
	reContainerPath = regexp.MustCompile(`full-path\s*=\s*["']([^"']+)["']`)
	reHTMLImage     = regexp.MustCompile(`(?is)<(?:img|image)\b[^>]*?(?:src|href)\s*=\s*["']([^"']+)["']`)
)

// epubPages returns image pages in spine order (each XHTML page referencing
// its image), falling back to manifest images, then to nil so the caller can
// use every image in the archive.
func epubPages(zr *zip.Reader) []pageEntry {
	files := map[string]*zip.File{}
	lower := map[string]*zip.File{}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		n := normZipName(f.Name)
		files[n] = f
		lower[strings.ToLower(n)] = f
	}
	find := func(n string) *zip.File {
		if f, ok := files[n]; ok {
			return f
		}
		return lower[strings.ToLower(n)]
	}
	resolve := func(base, href string) string {
		if i := strings.IndexAny(href, "#?"); i >= 0 {
			href = href[:i]
		}
		if u, err := url.PathUnescape(href); err == nil {
			href = u
		}
		if strings.HasPrefix(href, "/") {
			return path.Clean(strings.TrimPrefix(href, "/"))
		}
		return path.Join(base, href)
	}

	cf := find("META-INF/container.xml")
	if cf == nil {
		return nil
	}
	cdata, err := readZipFile(cf, 1<<20)
	if err != nil {
		return nil
	}
	m := reContainerPath.FindSubmatch(cdata)
	if m == nil {
		return nil
	}
	opfPath := string(m[1])
	of := find(opfPath)
	if of == nil {
		return nil
	}
	odata, err := readZipFile(of, 4<<20)
	if err != nil {
		return nil
	}
	var pkg opfPackage
	if err := xml.Unmarshal(odata, &pkg); err != nil {
		return nil
	}
	opfDir := path.Dir(opfPath)

	type item struct{ href, mt string }
	manifest := map[string]item{}
	for _, it := range pkg.Manifest {
		manifest[it.ID] = item{href: resolve(opfDir, it.Href), mt: strings.ToLower(it.MediaType)}
	}

	var pgs []pageEntry
	seen := map[string]bool{}
	add := func(name string) {
		if seen[name] {
			return
		}
		if f := find(name); f != nil && usablePageName(name) {
			seen[name] = true
			pgs = append(pgs, pageEntry{name: name, file: f})
		}
	}

	for _, ref := range pkg.Spine {
		it, ok := manifest[ref.IDRef]
		if !ok {
			continue
		}
		if strings.HasPrefix(it.mt, "image/") {
			add(it.href)
			continue
		}
		page := find(it.href)
		if page == nil {
			continue
		}
		data, err := readZipFile(page, 2<<20)
		if err != nil {
			continue
		}
		pageDir := path.Dir(it.href)
		for _, mm := range reHTMLImage.FindAllSubmatch(data, -1) {
			add(resolve(pageDir, string(mm[1])))
		}
	}
	if len(pgs) > 0 {
		return pgs
	}

	// No spine images: use the manifest's image items in natural order.
	var names []string
	for _, it := range pkg.Manifest {
		if strings.HasPrefix(strings.ToLower(it.MediaType), "image/") {
			names = append(names, resolve(opfDir, it.Href))
		}
	}
	sort.Slice(names, func(i, j int) bool { return NaturalLess(names[i], names[j]) })
	for _, n := range names {
		add(n)
	}
	return pgs
}

// ---------------------------------------------------------------------------
// Reader cache: keeps recently used zip central directories open so paging
// through a chapter doesn't re-parse the archive for every image.
// ---------------------------------------------------------------------------

type zipEntry struct {
	path     string
	mtime    int64
	size     int64
	rc       *zip.ReadCloser
	pages    []pageEntry
	refs     int
	evicted  bool
	lastUsed time.Time
	elem     *list.Element
}

type zipCache struct {
	mu      sync.Mutex
	max     int
	ttl     time.Duration
	entries map[string]*zipEntry
	lru     *list.List // front = most recently used
}

var archives = newZipCache(24, 30*time.Second)

func newZipCache(max int, ttl time.Duration) *zipCache {
	c := &zipCache{max: max, ttl: ttl, entries: map[string]*zipEntry{}, lru: list.New()}
	go c.janitor()
	return c
}

// janitor closes idle archives so library files are not held open (this
// matters on Windows/SMB, where open handles can block other writers).
func (c *zipCache) janitor() {
	for {
		time.Sleep(c.ttl / 2)
		c.mu.Lock()
		c.sweepLocked()
		c.mu.Unlock()
	}
}

// CloseArchives closes every cached archive (call on shutdown).
func CloseArchives() { archives.closeAll() }

func (c *zipCache) key(p, format string) string { return format + "|" + p }

func (c *zipCache) acquire(p, format string) (*zipEntry, error) {
	st, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	key := c.key(p, format)
	mtime, size := st.ModTime().UnixNano(), st.Size()

	c.mu.Lock()
	c.sweepLocked()
	if e, ok := c.entries[key]; ok {
		if e.mtime == mtime && e.size == size {
			e.refs++
			e.lastUsed = time.Now()
			c.lru.MoveToFront(e.elem)
			c.mu.Unlock()
			return e, nil
		}
		c.evictLocked(e)
	}
	c.mu.Unlock()

	rc, err := zip.OpenReader(p)
	if err != nil {
		return nil, err
	}
	pages := buildZipPages(&rc.Reader, format)
	if len(pages) == 0 {
		rc.Close()
		return nil, ErrNoPages
	}
	ne := &zipEntry{path: p, mtime: mtime, size: size, rc: rc, pages: pages, refs: 1, lastUsed: time.Now()}

	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[key]; ok && e.mtime == mtime && e.size == size {
		// Another goroutine won the race; use its entry.
		rc.Close()
		e.refs++
		e.lastUsed = time.Now()
		c.lru.MoveToFront(e.elem)
		return e, nil
	}
	ne.elem = c.lru.PushFront(key)
	c.entries[key] = ne
	for c.lru.Len() > c.max {
		back := c.lru.Back()
		if old, ok := c.entries[back.Value.(string)]; ok && old != ne {
			c.evictLocked(old)
		} else {
			break
		}
	}
	return ne, nil
}

func (c *zipCache) release(e *zipEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e.refs--
	if e.evicted && e.refs <= 0 {
		e.rc.Close()
	}
}

// evictLocked removes e from the cache; the file closes once unreferenced.
func (c *zipCache) evictLocked(e *zipEntry) {
	if e.evicted {
		return
	}
	e.evicted = true
	if e.elem != nil {
		key := e.elem.Value.(string)
		c.lru.Remove(e.elem)
		if c.entries[key] == e {
			delete(c.entries, key)
		}
	}
	if e.refs <= 0 {
		e.rc.Close()
	}
}

func (c *zipCache) sweepLocked() {
	now := time.Now()
	for _, e := range c.entries {
		if e.refs <= 0 && now.Sub(e.lastUsed) > c.ttl {
			c.evictLocked(e)
		}
	}
}

func (c *zipCache) closeAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.entries {
		c.evictLocked(e)
	}
}

// zipPageReader releases the cache entry when the page stream is closed.
type zipPageReader struct {
	io.ReadCloser
	entry *zipEntry
	once  sync.Once
}

func (z *zipPageReader) Close() error {
	err := z.ReadCloser.Close()
	z.once.Do(func() { archives.release(z.entry) })
	return err
}
