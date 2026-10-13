// Package pages renders the static HTML pages of the server: the index of
// figures and an info page per figure. All pages share one base template, take
// no data from requests and are rendered once by New.
package pages

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io/fs"

	"github.com/z0rr0/image89/internal/figure"
)

// baseTemplate is the layout every page fills with its "title" and "content".
const baseTemplate = "base.html"

//go:embed templates
var templates embed.FS

// Pages holds the rendered pages. It is safe for concurrent use.
type Pages struct {
	index Page
	info  map[figure.Kind]Page
}

// Page is a rendered page. Body is shared and must not be modified.
type Page struct {
	Body []byte
	// ETag is the strong entity tag of Body, quoted.
	ETag string
}

// entry is a figure on the index page.
type entry struct {
	Kind figure.Kind
	Name string
	// Info tells whether the figure has an info page.
	Info bool
}

// data is the data of every template.
type data struct {
	Version string
	// Kind and Name describe the figure of an info page.
	Kind figure.Kind
	Name string
	// Kinds lists the figures of the index page.
	Kinds []entry
}

// New renders the pages: the index lists kinds in the given order, and every
// figure kind with a template in templates/info gets an info page. version
// goes to the footer of every page.
func New(version string, kinds []figure.Kind) (*Pages, error) {
	return newPages(templates, version, kinds)
}

// newPages renders the pages from the templates directory of fsys.
func newPages(fsys fs.FS, version string, kinds []figure.Kind) (*Pages, error) {
	base, err := template.ParseFS(fsys, "templates/"+baseTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse base template: %w", err)
	}

	p := &Pages{info: make(map[figure.Kind]Page)}
	for _, kind := range figure.Kinds() {
		name := "templates/info/" + string(kind) + ".html"
		if _, err = fs.Stat(fsys, name); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		page, renderErr := execute(fsys, base, name, data{Version: version, Kind: kind, Name: figureName(kind)})
		if renderErr != nil {
			return nil, renderErr
		}
		p.info[kind] = page
	}

	entries := make([]entry, 0, len(kinds))
	for _, kind := range kinds {
		_, info := p.info[kind]
		entries = append(entries, entry{Kind: kind, Name: figureName(kind), Info: info})
	}
	if p.index, err = execute(fsys, base, "templates/index.html", data{Version: version, Kinds: entries}); err != nil {
		return nil, err
	}
	return p, nil
}

// execute renders the page template name of fsys on a copy of base.
func execute(fsys fs.FS, base *template.Template, name string, d data) (Page, error) {
	t, err := base.Clone()
	if err != nil {
		return Page{}, fmt.Errorf("clone base template for %s: %w", name, err)
	}
	if _, err = t.ParseFS(fsys, name); err != nil {
		return Page{}, fmt.Errorf("parse template %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err = t.ExecuteTemplate(&buf, baseTemplate, d); err != nil {
		return Page{}, fmt.Errorf("execute template %s: %w", name, err)
	}
	return Page{Body: buf.Bytes(), ETag: etag(buf.Bytes())}, nil
}

// etag returns the strong entity tag of body: the first 8 bytes of its SHA-256
// in hex, quoted. The hash covers the version, the kinds and the templates.
func etag(body []byte) string {
	sum := sha256.Sum256(body)
	return `"` + hex.EncodeToString(sum[:8]) + `"`
}

// Index returns the index page.
func (p *Pages) Index() Page {
	return p.index
}

// Info returns the info page of kind, or figure.ErrUnknownKind if it has none.
func (p *Pages) Info(kind figure.Kind) (Page, error) {
	page, ok := p.info[kind]
	if !ok {
		return Page{}, errors.Join(figure.ErrUnknownKind, fmt.Errorf("%q has no info page", kind))
	}
	return page, nil
}

// figureName returns the human-readable name of a figure kind.
func figureName(kind figure.Kind) string {
	switch kind {
	case figure.Mandelbrot:
		return "Mandelbrot set"
	case figure.Julia:
		return "Julia set"
	case figure.Newton:
		return "Newton fractal"
	case figure.Sierpinski:
		return "Sierpiński triangle"
	case figure.Koch:
		return "Koch snowflake"
	case figure.Lissajous:
		return "Lissajous curve"
	default:
		return string(kind)
	}
}
