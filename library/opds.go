package library

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	opdsNavType = "application/atom+xml;profile=opds-catalog;kind=navigation"
	opdsAcqType = "application/atom+xml;profile=opds-catalog;kind=acquisition"
	opdsPageLen = 50
)

func xmlEsc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

type opdsFeed struct {
	buf bytes.Buffer
}

func newOPDSFeed(id, title, self, kind string, updated time.Time) *opdsFeed {
	f := &opdsFeed{}
	f.buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	f.buf.WriteString(`<feed xmlns="http://www.w3.org/2005/Atom" xmlns:dc="http://purl.org/dc/terms/" ` +
		`xmlns:opds="http://opds-spec.org/2010/catalog" xmlns:pse="http://vaemendis.net/opds-pse/ns" ` +
		`xmlns:opensearch="http://a9.com/-/spec/opensearch/1.1/">` + "\n")
	fmt.Fprintf(&f.buf, "<id>%s</id>\n<title>%s</title>\n<updated>%s</updated>\n",
		xmlEsc(id), xmlEsc(title), updated.UTC().Format(time.RFC3339))
	f.link("self", self, kind, "")
	f.link("start", "/opds", opdsNavType, "")
	f.link("search", "/opds/search.xml", "application/opensearchdescription+xml", "")
	return f
}

func (f *opdsFeed) link(rel, href, typ, extra string) {
	fmt.Fprintf(&f.buf, `<link rel="%s" href="%s" type="%s"%s/>`+"\n", xmlEsc(rel), xmlEsc(href), xmlEsc(typ), extra)
}

func (f *opdsFeed) raw(s string) { f.buf.WriteString(s) }

func (f *opdsFeed) bytes() []byte {
	f.buf.WriteString("</feed>\n")
	return f.buf.Bytes()
}

func writeOPDS(w http.ResponseWriter, kind string, body []byte) {
	w.Header().Set("Content-Type", kind+"; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-cache")
	_, _ = w.Write(body)
}

func navEntry(id, title, content, href string, updated time.Time) string {
	return fmt.Sprintf("<entry><id>%s</id><title>%s</title><updated>%s</updated><content type=\"text\">%s</content>"+
		"<link rel=\"subsection\" href=\"%s\" type=\"%s\"/></entry>\n",
		xmlEsc(id), xmlEsc(title), updated.UTC().Format(time.RFC3339), xmlEsc(content), xmlEsc(href), xmlEsc(opdsNavType))
}

// OPDSRoot is the OPDS start page. With ?q= it behaves like a search.
func (h *Handler) OPDSRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("q") != "" {
		h.OPDSBrowse(w, r)
		return
	}
	now := time.Now()
	f := newOPDSFeed("urn:yomi-reader:root", "Yomi Reader", "/opds", opdsNavType, now)
	f.raw(navEntry("urn:yomi-reader:recent", "Recently added", "Newest series", "/opds/browse?sort=added", now))
	f.raw(navEntry("urn:yomi-reader:updated", "Recently updated", "Series with new chapters", "/opds/browse?sort=updated", now))
	f.raw(navEntry("urn:yomi-reader:all", "All series", "Alphabetical catalog", "/opds/browse?sort=title", now))
	f.raw(navEntry("urn:yomi-reader:favorites", "Favorites", "Your favorite series", "/opds/browse?favorites=1&sort=title", now))
	writeOPDS(w, "application/atom+xml;profile=opds-catalog;kind=navigation", f.bytes())
}

// OPDSSearchDescription serves the OpenSearch description document.
func (h *Handler) OPDSSearchDescription(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/opensearchdescription+xml; charset=utf-8")
	_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<OpenSearchDescription xmlns="http://a9.com/-/spec/opensearch/1.1/">
  <ShortName>Yomi Reader</ShortName>
  <Description>Search the Yomi Reader catalog</Description>
  <InputEncoding>UTF-8</InputEncoding>
  <Url type="application/atom+xml;profile=opds-catalog;kind=navigation" template="/opds/browse?q={searchTerms}"/>
</OpenSearchDescription>
`))
}

// OPDSBrowse lists series (navigation feed, each entry leads to a series feed).
func (h *Handler) OPDSBrowse(w http.ResponseWriter, r *http.Request) {
	v, ok := h.viewer(w, r)
	if !ok {
		return
	}
	qp := r.URL.Query()
	page := queryInt(r, "page", 1)
	if page < 1 {
		page = 1
	}
	q := SeriesQuery{
		Q:             qp.Get("q"),
		Tags:          splitCSV(qp.Get("tag")),
		Source:        qp.Get("source"),
		Sort:          qp.Get("sort"),
		Order:         qp.Get("order"),
		Offset:        (page - 1) * opdsPageLen,
		Limit:         opdsPageLen,
		OnlyFavorites: qp.Get("favorites") == "1",
	}
	res, err := h.store.ListSeries(v, q)
	if err != nil {
		h.internalErr(w, err)
		return
	}

	pageURL := func(p int) string {
		u := url.Values{}
		for k, vals := range qp {
			if k != "page" {
				u[k] = vals
			}
		}
		u.Set("page", strconv.Itoa(p))
		return "/opds/browse?" + u.Encode()
	}

	title := "Series"
	if q.Q != "" {
		title = "Search: " + q.Q
	}
	f := newOPDSFeed("urn:yomi-reader:browse:"+r.URL.RawQuery, title, pageURL(page), opdsNavType, time.Now())
	if page > 1 {
		f.link("previous", pageURL(page-1), opdsNavType, "")
	}
	if res.Offset+len(res.Items) < res.Total {
		f.link("next", pageURL(page+1), opdsNavType, "")
	}
	fmt.Fprintf(&f.buf, "<opensearch:totalResults>%d</opensearch:totalResults><opensearch:itemsPerPage>%d</opensearch:itemsPerPage>\n",
		res.Total, opdsPageLen)

	for _, se := range res.Items {
		f.raw("<entry>")
		fmt.Fprintf(&f.buf, "<id>urn:uuid:%s</id><title>%s</title><updated>%s</updated>",
			xmlEsc(se.ID), xmlEsc(se.Title), se.UpdatedAt.UTC().Format(time.RFC3339))
		writeSeriesMeta(&f.buf, se)
		fmt.Fprintf(&f.buf, `<link rel="subsection" href="/opds/series/%s" type="%s"/>`, xmlEsc(se.ID), xmlEsc(opdsAcqType))
		writeCoverLinks(&f.buf, se.ID)
		f.raw("</entry>\n")
	}
	writeOPDS(w, "application/atom+xml;profile=opds-catalog;kind=navigation", f.bytes())
}

func writeSeriesMeta(b *bytes.Buffer, se *Series) {
	if se.Authors != "" {
		for _, a := range splitCSV(se.Authors) {
			fmt.Fprintf(b, "<author><name>%s</name></author>", xmlEsc(a))
		}
	}
	sum := se.Summary
	if sum == "" {
		sum = fmt.Sprintf("%d chapters", se.ChapterCount)
	}
	fmt.Fprintf(b, `<summary type="text">%s</summary>`, xmlEsc(sum))
	for _, g := range se.Genres {
		fmt.Fprintf(b, `<category term="%s" label="%s"/>`, xmlEsc(g), xmlEsc(g))
	}
	if se.Year > 0 {
		fmt.Fprintf(b, "<dc:issued>%d</dc:issued>", se.Year)
	}
}

func writeCoverLinks(b *bytes.Buffer, seriesID string) {
	href := "/opds/covers/" + xmlEsc(seriesID)
	fmt.Fprintf(b, `<link rel="http://opds-spec.org/image" href="%s" type="image/jpeg"/>`, href)
	fmt.Fprintf(b, `<link rel="http://opds-spec.org/image/thumbnail" href="%s" type="image/jpeg"/>`, href)
}

var formatMIME = map[string]string{
	FormatCBZ:    "application/vnd.comicbook+zip",
	FormatFolder: "application/vnd.comicbook+zip",
	FormatEPUB:   "application/epub+zip",
	FormatPDF:    "application/pdf",
}

// OPDSSeries is the acquisition feed of a series: one entry per merged chapter.
func (h *Handler) OPDSSeries(w http.ResponseWriter, r *http.Request) {
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
	merged, err := h.mergedChapters(se.ID)
	if err != nil {
		h.internalErr(w, err)
		return
	}

	f := newOPDSFeed("urn:uuid:"+se.ID, se.Title, "/opds/series/"+se.ID, opdsAcqType, se.UpdatedAt)
	f.link("up", "/opds/browse?sort=title", opdsNavType, "")
	for _, c := range merged {
		title := c.Title
		if c.Number >= 0 && !strings.Contains(strings.ToLower(title), "ch") {
			title = "Chapter " + strconv.FormatFloat(c.Number, 'f', -1, 64) + " - " + title
		}
		f.raw("<entry>")
		fmt.Fprintf(&f.buf, "<id>urn:uuid:%s</id><title>%s</title><updated>%s</updated>",
			xmlEsc(c.ID), xmlEsc(title), c.ScannedAt.UTC().Format(time.RFC3339))
		writeSeriesMeta(&f.buf, se)
		fmt.Fprintf(&f.buf, `<content type="text">%s</content>`, xmlEsc(fmt.Sprintf("%s - %d pages (%s)", se.Title, c.PageCount, c.SourceName)))
		mt := formatMIME[c.Format]
		fmt.Fprintf(&f.buf, `<link rel="http://opds-spec.org/acquisition" href="/opds/chapters/%s/file" type="%s" length="%d"/>`,
			xmlEsc(c.ID), xmlEsc(mt), c.SizeBytes)
		if c.Format != FormatPDF && c.PageCount > 0 {
			// Page Streaming Extension: lets clients read without downloading.
			fmt.Fprintf(&f.buf, `<link rel="http://vaemendis.net/opds-pse/stream" href="/opds/chapters/%s/page/{pageNumber}" type="image/jpeg" pse:count="%d"/>`,
				xmlEsc(c.ID), c.PageCount)
		}
		writeCoverLinks(&f.buf, se.ID)
		f.raw("</entry>\n")
	}
	writeOPDS(w, "application/atom+xml;profile=opds-catalog;kind=acquisition", f.bytes())
}
