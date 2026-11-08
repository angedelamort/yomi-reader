package library

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	_ "image/gif" // register GIF decoding for cover uploads
	"image/jpeg"
	_ "image/png" // register PNG decoding for cover uploads
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp" // register WebP decoding for covers
)

// maxCoverSourceBytes caps how much of a page image is read for a cover.
const maxCoverSourceBytes = 96 << 20

// CoverService generates and caches series covers under
// DATA_PATH/cache/covers/{seriesID}.jpg from the first page of the lowest
// chapter. It never writes into library roots.
type CoverService struct {
	store *Store
	dir   string
	width int

	queue chan string

	mu       sync.Mutex
	queued   map[string]bool
	inflight map[string]*coverCall

	wg   sync.WaitGroup
	stop chan struct{}
	once sync.Once
}

type coverCall struct {
	done chan struct{}
	path string
	err  error
}

// NewCoverService creates a service writing into dir with the given width.
func NewCoverService(store *Store, dir string, width int) *CoverService {
	_ = os.MkdirAll(dir, 0o755)
	return &CoverService{
		store:    store,
		dir:      dir,
		width:    width,
		queue:    make(chan string, 4096),
		queued:   map[string]bool{},
		inflight: map[string]*coverCall{},
		stop:     make(chan struct{}),
	}
}

// Start launches background workers that drain the enqueue queue.
func (c *CoverService) Start(workers int) {
	if workers < 1 {
		workers = 1
	}
	for i := 0; i < workers; i++ {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			for {
				select {
				case <-c.stop:
					return
				case id := <-c.queue:
					c.mu.Lock()
					delete(c.queued, id)
					c.mu.Unlock()
					if _, err := c.Ensure(id); err != nil {
						log.Printf("cover %s: %v", id, err)
					}
				}
			}
		}()
	}
}

// Stop terminates the workers.
func (c *CoverService) Stop() {
	c.once.Do(func() { close(c.stop) })
	c.wg.Wait()
}

// Path returns where the cover of a series is (or will be) cached.
func (c *CoverService) Path(seriesID string) string {
	return filepath.Join(c.dir, seriesID+".jpg")
}

// Has reports whether the cover is already cached.
func (c *CoverService) Has(seriesID string) bool {
	st, err := os.Stat(c.Path(seriesID))
	return err == nil && st.Size() > 0
}

// Remove deletes a cached cover.
func (c *CoverService) Remove(seriesID string) { _ = os.Remove(c.Path(seriesID)) }

// Enqueue schedules asynchronous cover generation. If the queue is full the
// request is dropped; the cover is then generated lazily on first request.
func (c *CoverService) Enqueue(seriesID string) {
	c.mu.Lock()
	if c.queued[seriesID] {
		c.mu.Unlock()
		return
	}
	c.queued[seriesID] = true
	c.mu.Unlock()
	select {
	case c.queue <- seriesID:
	default:
		c.mu.Lock()
		delete(c.queued, seriesID)
		c.mu.Unlock()
	}
}

// Ensure returns the cached cover path, generating it first if needed.
// Concurrent calls for the same series share one generation.
func (c *CoverService) Ensure(seriesID string) (string, error) {
	if c.Has(seriesID) {
		return c.Path(seriesID), nil
	}
	c.mu.Lock()
	if call, ok := c.inflight[seriesID]; ok {
		c.mu.Unlock()
		<-call.done
		return call.path, call.err
	}
	call := &coverCall{done: make(chan struct{})}
	c.inflight[seriesID] = call
	c.mu.Unlock()

	call.path, call.err = c.generate(seriesID)
	close(call.done)

	c.mu.Lock()
	delete(c.inflight, seriesID)
	c.mu.Unlock()
	return call.path, call.err
}

func (c *CoverService) generate(seriesID string) (string, error) {
	ref, err := c.store.CoverChapter(seriesID)
	if err != nil {
		return "", err
	}
	var img image.Image
	if ref.Format != FormatPDF {
		if full, err := ResolveUnderRoot(ref.LibraryRoot, ref.FSPath); err == nil {
			img, _ = firstDecodablePage(full, ref.Format, 3)
		}
	}

	var out image.Image
	if img != nil {
		out = img
	} else {
		var title string
		_ = c.store.db.QueryRow(`SELECT title FROM series WHERE id = ?`, seriesID).Scan(&title)
		out = placeholderCover(title, c.width, c.width*3/2)
	}
	return c.writeCover(seriesID, out)
}

// WriteImage crops/scales img to the cover size and locks it as a manual override.
func (c *CoverService) WriteImage(seriesID string, img image.Image) error {
	if img == nil {
		return errors.New("empty image")
	}
	if _, err := c.writeCover(seriesID, img); err != nil {
		return err
	}
	return c.store.SetCoverLocked(seriesID, true)
}

// GenerateFromChapter sets the series cover from the first page of chapterID and locks it.
func (c *CoverService) GenerateFromChapter(seriesID, chapterID string) error {
	ref, err := c.store.GetChapterRef(chapterID)
	if err != nil {
		return err
	}
	if ref.SeriesID != seriesID {
		return ErrForbidden
	}
	if ref.Format == FormatPDF {
		return errors.New("PDF chapters can't be used as covers — upload an image instead")
	}
	full, err := ResolveUnderRoot(ref.LibraryRoot, ref.FSPath)
	if err != nil {
		return errors.New("chapter file is missing or outside the library")
	}
	img, err := firstDecodablePage(full, ref.Format, 3)
	if err != nil {
		return err
	}
	return c.WriteImage(seriesID, img)
}

// ResetAuto unlocks the cover and regenerates it from the lowest chapter.
func (c *CoverService) ResetAuto(seriesID string) error {
	if err := c.store.SetCoverLocked(seriesID, false); err != nil {
		return err
	}
	c.Remove(seriesID)
	_, err := c.generate(seriesID)
	return err
}

func (c *CoverService) writeCover(seriesID string, img image.Image) (string, error) {
	out := fillCover(img, c.width, c.width*3/2)
	tmp := c.Path(seriesID) + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	if err := jpeg.Encode(f, out, &jpeg.Options{Quality: 82}); err != nil {
		f.Close()
		os.Remove(tmp)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, c.Path(seriesID)); err != nil {
		os.Remove(tmp)
		return "", err
	}
	c.store.setSeriesCover(seriesID, seriesID+".jpg")
	return c.Path(seriesID), nil
}

// firstDecodablePage decodes the first of the first n pages that is a valid image.
func firstDecodablePage(path, format string, n int) (image.Image, error) {
	var lastErr error
	var sawHTML, sawAVIF bool
	for i := 0; i < n; i++ {
		rc, _, _, err := OpenPage(path, format, i)
		if err != nil {
			lastErr = err
			continue
		}
		data, err := io.ReadAll(io.LimitReader(rc, maxCoverSourceBytes))
		rc.Close()
		if err != nil {
			lastErr = err
			continue
		}
		switch sniffPageKind(data) {
		case "html":
			sawHTML = true
			continue
		case "avif":
			sawAVIF = true
			continue
		}
		img, err := imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
		if err == nil {
			return img, nil
		}
		lastErr = err
	}
	switch {
	case sawAVIF:
		return nil, errors.New("chapter uses AVIF images, which can't be used for covers — upload a JPEG/PNG or pick another chapter")
	case sawHTML:
		return nil, errors.New("chapter pages are not valid images (download may have failed) — try another chapter or upload a cover")
	case lastErr != nil && errors.Is(lastErr, zip.ErrAlgorithm):
		return nil, errors.New("chapter archive uses an unsupported compression method")
	case lastErr != nil:
		return nil, fmt.Errorf("could not decode cover page: %w", lastErr)
	default:
		return nil, errors.New("could not decode a cover page from that chapter")
	}
}

// sniffPageKind classifies page bytes before attempting image decode.
func sniffPageKind(data []byte) string {
	trim := bytes.TrimLeft(data, " \t\r\n")
	if len(trim) >= 5 {
		low := strings.ToLower(string(trim[:min(32, len(trim))]))
		if strings.HasPrefix(low, "<!doctype") || strings.HasPrefix(low, "<html") || strings.HasPrefix(low, "<head") {
			return "html"
		}
	}
	// ISO BMFF: ....ftyp....avif / avis / avif
	if len(data) >= 12 && string(data[4:8]) == "ftyp" {
		brand := string(data[8:12])
		if brand == "avif" || brand == "avis" || brand == "mif1" || brand == "miaf" {
			return "avif"
		}
		if len(data) >= 16 {
			compat := string(data[8:min(24, len(data))])
			if strings.Contains(compat, "avif") || strings.Contains(compat, "avis") {
				return "avif"
			}
		}
	}
	return ""
}

// fillCover crops (top-anchored) and scales img to exactly w x h. Very tall
// webtoon strips are cropped first so only the top is resampled.
func fillCover(img image.Image, w, h int) image.Image {
	b := img.Bounds()
	if b.Dx() > 0 && b.Dy() > b.Dx()*3/2 {
		img = imaging.Crop(img, image.Rect(b.Min.X, b.Min.Y, b.Max.X, b.Min.Y+b.Dx()*3/2))
	}
	return imaging.Fill(img, w, h, imaging.Top, imaging.Lanczos)
}

// placeholderCover draws a flat, title-derived color block with a lighter
// gradient (no text rendering dependency).
func placeholderCover(title string, w, h int) image.Image {
	hs := fnv.New32a()
	_, _ = hs.Write([]byte(title))
	v := hs.Sum32()
	base := color.RGBA{R: uint8(60 + v&0x5f), G: uint8(60 + (v>>8)&0x5f), B: uint8(80 + (v>>16)&0x5f), A: 255}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		f := float64(y) / float64(h)
		c := color.RGBA{
			R: uint8(float64(base.R) * (1.2 - 0.5*f)),
			G: uint8(float64(base.G) * (1.2 - 0.5*f)),
			B: uint8(float64(base.B) * (1.2 - 0.5*f)),
			A: 255,
		}
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}
