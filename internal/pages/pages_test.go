package pages

import (
	"errors"
	"io/fs"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/z0rr0/image89/internal/figure"
)

// githubLink is the link to the repository in the footer of every page.
const githubLink = `<a href="https://github.com/z0rr0/image89">GitHub</a>`

func TestIndex(t *testing.T) {
	t.Parallel()

	kinds := []figure.Kind{figure.Mandelbrot, figure.Julia, figure.Newton}
	p, err := New("v1.2.3", kinds)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	index := string(p.Index().Body)

	for _, want := range []string{
		"<!doctype html>", `<html lang="en">`, "<title>Image89</title>", "Image89 v1.2.3", githubLink, "</html>",
	} {
		if !strings.Contains(index, want) {
			t.Errorf("index does not contain %q", want)
		}
	}
	for _, kind := range kinds {
		for _, want := range []string{
			`<img loading="lazy" src="/` + string(kind) + `/320x240/" width="320" height="240" alt="` + figureName(kind) + `">`,
			`<a href="/` + string(kind) + `/">`,
			`<a href="/` + string(kind) + `/640x480/?format=jpeg">`,
			"<h2>" + figureName(kind) + "</h2>",
		} {
			if !strings.Contains(index, want) {
				t.Errorf("index does not contain %q", want)
			}
		}
	}
	// Only the kinds given to New are listed, each with its info page.
	if strings.Contains(index, "/koch/") {
		t.Error("index lists koch, which was not given")
	}
	for _, kind := range kinds {
		if link := `<a href="/` + string(kind) + `/info/">About</a>`; !strings.Contains(index, link) {
			t.Errorf("index has no link %q", link)
		}
	}
}

func TestVersionEscaped(t *testing.T) {
	t.Parallel()

	p, err := New("<b>v1</b>", nil)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	info, err := p.Info(figure.Mandelbrot)
	if err != nil {
		t.Fatalf("Info error: %v", err)
	}
	for name, page := range map[string][]byte{"index": p.Index().Body, "info": info.Body} {
		if s := string(page); strings.Contains(s, "<b>v1</b>") || !strings.Contains(s, "&lt;b&gt;v1&lt;/b&gt;") {
			t.Errorf("%s page does not escape the version", name)
		}
	}
}

// tags matches HTML tags and the MathML formulas, which are not prose.
var tags = regexp.MustCompile(`(?s)<math.*?</math>|<[^>]*>`)

func TestInfoPages(t *testing.T) {
	t.Parallel()

	p, err := New("v1", nil)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	for _, kind := range figure.Kinds() {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()

			page, err := p.Info(kind)
			if err != nil {
				t.Fatalf("Info error: %v", err)
			}
			s, name := string(page.Body), figureName(kind)
			for _, want := range []string{
				"<title>" + name + " · Image89</title>", "<h1>" + name + "</h1>", `<math display="block">`,
				`<img src="/` + string(kind) + `/640x480/"`, `<a href="/` + string(kind) + `/">`, "Image89 v1", githubLink,
			} {
				if !strings.Contains(s, want) {
					t.Errorf("info page does not contain %q", want)
				}
			}

			_, main, _ := strings.Cut(s, "<main>")
			main, _, _ = strings.Cut(main, "</main>")
			if words := len(strings.Fields(tags.ReplaceAllString(main, " "))); words < 300 || words > 500 {
				t.Errorf("info page has %d words, want 300–500", words)
			}
		})
	}
}

func TestInfoUnknown(t *testing.T) {
	t.Parallel()

	p, err := New("v1", nil)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	for _, kind := range []figure.Kind{"", "nope", "Mandelbrot"} {
		page, err := p.Info(kind)
		if !errors.Is(err, figure.ErrUnknownKind) || page.Body != nil || page.ETag != "" {
			t.Errorf("Info(%q) = %d bytes, ETag %q, %v; want %v", kind, len(page.Body), page.ETag, err, figure.ErrUnknownKind)
		}
	}
}

func TestETag(t *testing.T) {
	t.Parallel()

	render := func(fsys fs.FS, version string, kinds []figure.Kind) (Page, Page) {
		t.Helper()
		p, err := newPages(fsys, version, kinds)
		if err != nil {
			t.Fatalf("newPages error: %v", err)
		}
		info, err := p.Info(figure.Mandelbrot)
		if err != nil {
			t.Fatalf("Info error: %v", err)
		}
		return p.Index(), info
	}
	index1, info1 := render(templates, "v1", figure.Kinds())
	index1again, info1again := render(templates, "v1", figure.Kinds())
	index2, info2 := render(templates, "v2", figure.Kinds())
	indexKinds, _ := render(templates, "v1", []figure.Kind{figure.Mandelbrot})

	// The same version with another template: a dev build keeps v0.0.0.
	files := fstest.MapFS{
		"templates/base.html":            {Data: []byte(`{{block "content" .}}{{end}}`)},
		"templates/index.html":           {Data: []byte(`{{define "content"}}index {{.Version}}{{end}}`)},
		"templates/info/mandelbrot.html": {Data: []byte(`{{define "content"}}info {{.Version}}{{end}}`)},
	}
	indexA, infoA := render(files, "v1", nil)
	files["templates/info/mandelbrot.html"] = &fstest.MapFile{Data: []byte(`{{define "content"}}new info {{.Version}}{{end}}`)}
	indexB, infoB := render(files, "v1", nil)

	tests := []struct {
		name string
		a, b Page
		same bool
	}{
		{name: "index of one version", a: index1, b: index1again, same: true},
		{name: "info of one version", a: info1, b: info1again, same: true},
		{name: "index of another version", a: index1, b: index2},
		{name: "info of another version", a: info1, b: info2},
		{name: "index of other kinds", a: index1, b: indexKinds},
		{name: "index and info", a: index1, b: info1},
		{name: "index with the same templates", a: indexA, b: indexB, same: true},
		{name: "info of another template", a: infoA, b: infoB},
	}
	quoted := regexp.MustCompile(`^"[0-9a-f]{16}"$`)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for _, page := range []Page{tt.a, tt.b} {
				if !quoted.MatchString(page.ETag) {
					t.Errorf("ETag = %q, want 16 hex digits in quotes", page.ETag)
				}
			}
			if same := tt.a.ETag == tt.b.ETag; same != tt.same {
				t.Errorf("ETags %q and %q: equal = %v, want %v", tt.a.ETag, tt.b.ETag, same, tt.same)
			}
		})
	}
}

func TestFigureName(t *testing.T) {
	t.Parallel()

	names := make(map[string]bool)
	for _, kind := range figure.Kinds() {
		name := figureName(kind)
		if name == string(kind) || names[name] {
			t.Errorf("figureName(%q) = %q, want a distinct human-readable name", kind, name)
		}
		names[name] = true
	}
	if got := figureName("nope"); got != "nope" {
		t.Errorf(`figureName("nope") = %q, want "nope"`, got)
	}
}

func TestNewErrors(t *testing.T) {
	t.Parallel()

	const (
		base  = `<html><title>{{block "title" .}}x{{end}}</title>{{block "content" .}}{{end}}</html>`
		index = `{{define "content"}}{{range .Kinds}}{{.Name}}{{end}}{{end}}`
	)
	tests := []struct {
		name  string
		files fstest.MapFS
		want  string
	}{
		{
			name:  "no base",
			files: fstest.MapFS{"templates/index.html": {Data: []byte(index)}},
			want:  "parse base template",
		},
		{
			name:  "no index",
			files: fstest.MapFS{"templates/base.html": {Data: []byte(base)}},
			want:  "parse template templates/index.html",
		},
		{
			name: "broken info",
			files: fstest.MapFS{
				"templates/base.html":            {Data: []byte(base)},
				"templates/index.html":           {Data: []byte(index)},
				"templates/info/mandelbrot.html": {Data: []byte(`{{define "content"}}{{.Missing}}{{end}}`)},
			},
			want: "execute template templates/info/mandelbrot.html",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p, err := newPages(tt.files, "v1", []figure.Kind{figure.Mandelbrot})
			if err == nil || !strings.Contains(err.Error(), tt.want) || p != nil {
				t.Errorf("newPages error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestNewFindsInfoTemplates(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"templates/base.html":       {Data: []byte(`{{block "content" .}}{{end}}`)},
		"templates/index.html":      {Data: []byte(`{{define "content"}}{{range .Kinds}}{{.Kind}}:{{.Info}};{{end}}{{end}}`)},
		"templates/info/koch.html":  {Data: []byte(`{{define "content"}}{{.Name}}{{end}}`)},
		"templates/info/other.html": {Data: []byte(`{{define "content"}}other{{end}}`)},
	}
	p, err := newPages(files, "v1", []figure.Kind{figure.Koch, figure.Julia})
	if err != nil {
		t.Fatalf("newPages error: %v", err)
	}
	if got, want := string(p.Index().Body), "koch:true;julia:false;"; got != want {
		t.Errorf("index = %q, want %q", got, want)
	}
	if page, err := p.Info(figure.Koch); err != nil || string(page.Body) != "Koch snowflake" {
		t.Errorf("Info(koch) = %q, %v; want the koch page", page.Body, err)
	}
}
