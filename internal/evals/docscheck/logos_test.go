package docscheck

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// logoMaxBytes is the most a logo may weigh: 50 KB, where a KB is 1024 bytes, as in the site's own size caps.
const logoMaxBytes = 50 * 1024

// logoBannedElements are the elements a logo may not hold, by local name in lowercase, each with the reason. A prefix
// and the letter case do not matter: <svg:script> and <SCRIPT> are scripts too.
var logoBannedElements = []struct{ name, why string }{
	{"script", "it runs code"},
	{"foreignobject", "it embeds HTML"},
	{"image", "it loads or embeds a picture"},
	{"iframe", "it loads another page"},
}

// logoParts is what the rules read of a logo, collected in one walk over the tokens of its XML.
type logoParts struct {
	elements   []xml.StartElement // every element in document order; the first is the root of a well-formed file
	roots      int                // how many elements stand at the top of the file; a logo has one
	directives int                // how many <!...> declarations the file holds; a DOCTYPE is one
	styles     []string           // the text of each style element, its text nodes joined
	err        error              // why the walk stopped before the end of the file: it is not well-formed XML
}

// logoAttr is an attribute of a logo with the local name of the element it sits on.
type logoAttr struct {
	xml.Attr
	element string
}

// lowerLocal is the local name of an element or an attribute in lowercase: the prefix is no part of it, and the letter
// case does not matter.
func lowerLocal(n xml.Name) string { return strings.ToLower(n.Local) }

// logoRoot is the name a logo's root element has.
const logoRoot = "svg"

// root is the first element, the root of a well-formed file; ok is false when the file has none.
func (p logoParts) root() (root xml.StartElement, ok bool) {
	if len(p.elements) == 0 {
		return xml.StartElement{}, false
	}
	return p.elements[0], true
}

// svgRoot is the root element when it is an svg; ok is false when the file has no root or the root is something else.
func (p logoParts) svgRoot() (root xml.StartElement, ok bool) {
	root, ok = p.root()
	return root, ok && root.Name.Local == logoRoot
}

// attrs lists every attribute of every element, each with the element it sits on.
func (p logoParts) attrs() []logoAttr {
	var out []logoAttr
	for _, e := range p.elements {
		for _, a := range e.Attr {
			out = append(out, logoAttr{Attr: a, element: e.Name.Local})
		}
	}
	return out
}

// readLogo walks the XML tokens of a logo once. The walk stops at the first syntax error and keeps what it read before
// it, and the rules judge only what they find. A CDATA section arrives as text, so the style rules read it like any
// other text, and the text nodes of a style element are joined with the comments left out, the way a browser reads its
// CSS.
func readLogo(data []byte) logoParts {
	var (
		parts logoParts
		css   strings.Builder // the text of the style element being read
	)
	depth, styleDepth := 0, 0 // how deep the walk is, and the depth of the outermost style element it is in (0: none)
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				parts.err = err
			}
			return parts
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			parts.elements = append(parts.elements, t)
			if depth == 1 {
				parts.roots++
			}
			if styleDepth == 0 && lowerLocal(t.Name) == "style" {
				styleDepth = depth
			}
		case xml.EndElement:
			if depth == styleDepth {
				parts.styles = append(parts.styles, css.String())
				css.Reset()
				styleDepth = 0
			}
			depth--
		case xml.CharData:
			if styleDepth > 0 {
				css.Write(t)
			}
		case xml.Directive:
			parts.directives++
		}
	}
}

// checkLogo applies the logo rules of the design to the bytes of the file called name and returns one message per
// problem: a file of at most 50 KB, well-formed XML with one root svg that has a viewBox, no DOCTYPE, no script,
// foreignObject, image or iframe element, no style element that holds @import or url(, no on* attribute and no href
// that is not a #fragment. Every rule is a function of its own.
func checkLogo(name string, data []byte) []string {
	label, parts := fmt.Sprintf("logo %q", name), readLogo(data)
	return slices.Concat(
		checkLogoSize(label, data),
		checkLogoXML(label, parts),
		checkLogoRoot(label, parts),
		checkLogoViewBox(label, parts),
		checkLogoDoctype(label, parts),
		checkLogoElements(label, parts),
		checkLogoStyleImports(label, parts),
		checkLogoStyleURLs(label, parts),
		checkLogoEvents(label, parts),
		checkLogoHrefs(label, parts),
	)
}

// checkLogoSize: a logo is one small shape, so the file is at most 50 KB.
func checkLogoSize(label string, data []byte) []string {
	var p findings
	if len(data) > logoMaxBytes {
		p.addf("%s: over %d KB (%d bytes; a logo is one small shape)", label, logoMaxBytes/1024, len(data))
	}
	return p
}

// checkLogoXML: the file is XML with exactly one root element. The decoder lets a second root pass without a word, so
// the root count is made here; text outside the root it lets pass too, and that is not judged. Only UTF-8 is read: a
// file that declares another encoding is refused here as well.
func checkLogoXML(label string, parts logoParts) []string {
	var p findings
	switch {
	case parts.err != nil:
		p.addf("%s: not XML: %v", label, parts.err)
	case parts.roots == 0:
		p.addf("%s: not XML: no root element", label)
	case parts.roots > 1:
		p.addf("%s: not XML: %d root elements (a logo has one svg)", label, parts.roots)
	}
	return p
}

// checkLogoRoot: the root element is svg. XML names are case-sensitive, so a root in capitals is not an svg that a
// browser draws; a prefix (<svg:svg>) is only a spelling of the namespace.
func checkLogoRoot(label string, parts logoParts) []string {
	var p findings
	if root, ok := parts.root(); ok && root.Name.Local != logoRoot {
		p.addf("%s: root is not svg (the root element is <%s>)", label, root.Name.Local)
	}
	return p
}

// checkLogoViewBox: the root svg has a viewBox, which is what the page scales the logo by. The attribute is spelled
// viewBox, as XML names are case-sensitive and a viewbox is ignored. A root that is not svg is left to checkLogoRoot.
func checkLogoViewBox(label string, parts logoParts) []string {
	var p findings
	root, ok := parts.svgRoot()
	if ok && !slices.ContainsFunc(root.Attr, func(a xml.Attr) bool { return a.Name.Local == "viewBox" }) {
		p.addf("%s: no viewBox on the root svg (the page scales the logo by it)", label)
	}
	return p
}

// checkLogoDoctype: no <!...> declaration anywhere in the file. A DOCTYPE can define entities and load a DTD.
func checkLogoDoctype(label string, parts logoParts) []string {
	var p findings
	if parts.directives > 0 {
		p.addf("%s: doctype: a <!...> declaration is not allowed (a DOCTYPE can define entities and load a DTD)", label)
	}
	return p
}

// checkLogoElements: none of the banned elements, once per element name.
func checkLogoElements(label string, parts logoParts) []string {
	var p findings
	for _, banned := range logoBannedElements {
		if slices.ContainsFunc(parts.elements, func(e xml.StartElement) bool { return lowerLocal(e.Name) == banned.name }) {
			p.addf("%s: %s element (%s)", label, banned.name, banned.why)
		}
	}
	return p
}

// styleHolds reports whether the text of any style element holds needle, which is in lowercase: CSS reads @import and
// url( in any letter case.
func styleHolds(styles []string, needle string) bool {
	return slices.ContainsFunc(styles, func(css string) bool { return strings.Contains(strings.ToLower(css), needle) })
}

// checkLogoStyleImports: no style element holds @import.
func checkLogoStyleImports(label string, parts logoParts) []string {
	var p findings
	if styleHolds(parts.styles, "@import") {
		p.addf("%s: style import: a style element holds @import (it loads another stylesheet)", label)
	}
	return p
}

// checkLogoStyleURLs: no style element holds url(.
func checkLogoStyleURLs(label string, parts logoParts) []string {
	var p findings
	if styleHolds(parts.styles, "url(") {
		p.addf("%s: style url: a style element holds url( (it reaches another file)", label)
	}
	return p
}

// checkLogoEvents: no attribute whose name starts with on, which is how an event handler is written.
func checkLogoEvents(label string, parts logoParts) []string {
	var p findings
	for _, a := range parts.attrs() {
		if strings.HasPrefix(lowerLocal(a.Name), "on") {
			p.addf("%s: event attribute %s on <%s> (a logo has no on* attributes)", label, a.Name.Local, a.element)
		}
	}
	return p
}

// checkLogoHrefs: an href or xlink:href, with any prefix, points to a #fragment of the same file and to nothing else. A
// long value, such as a data: URI, is cut short in the message.
func checkLogoHrefs(label string, parts logoParts) []string {
	var p findings
	for _, a := range parts.attrs() {
		if lowerLocal(a.Name) == "href" && !strings.HasPrefix(a.Value, "#") {
			p.addf("%s: external href on <%s>: %.40q (an href may only point to a #fragment of the same file)",
				label, a.element, a.Value)
		}
	}
	return p
}

// checkLogoOwners: each logo file, given by its id (the file name without .svg), belongs to a sponsor of sponsors.json
// whose tier is shown with a logo. A logo of any other id, of a Lab hours sponsor or of a Supporter is an orphan. A
// sponsor whose credit has ended keeps its logo.
func checkLogoOwners(ids []string, sponsors []sponsor) []string {
	var p findings
	for _, id := range ids {
		i := slices.IndexFunc(sponsors, func(s sponsor) bool { return s.ID == id })
		switch {
		case i < 0:
			p.addf("orphan logo %s.svg: no sponsor %q in sponsors.json", id, id)
		case !needsLogo(sponsors[i].Tier):
			p.addf("orphan logo %s.svg: sponsor %q is %s, a tier shown without a logo", id, id, sponsors[i].Tier)
		}
	}
	return p
}

// logoOpen starts a clean logo: one root svg with a viewBox and the two namespaces a logo may use.
const logoOpen = `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" ` +
	`viewBox="0 0 10 10">`

// logoShape is the single path of a clean logo.
const logoShape = `<path d="M0 0h10v10H0z"/>`

// inLogo wraps a body in a clean root, so that a case shows only what it adds.
func inLogo(body string) string { return logoOpen + body + "</svg>" }

// cleanLogo follows every rule: one svg with a viewBox, holding one path.
var cleanLogo = inLogo(logoShape)

// paddedLogo is the clean logo followed by a comment of spaces, so that the file is exactly size bytes long.
func paddedLogo(size int) string {
	const wrapper = "<!---->"
	return cleanLogo + "<!--" + strings.Repeat(" ", size-len(cleanLogo)-len(wrapper)) + "-->"
}

// TestCheckLogoCatchesEachRule: a logo that follows the rules gives no problems; every other case breaks one rule and
// gets exactly one message, carrying the rule's key. The cases without a key are the edges that must stay clean.
func TestCheckLogoCatchesEachRule(t *testing.T) {
	const prefixed = `<svg:svg xmlns:svg="http://www.w3.org/2000/svg" viewBox="0 0 10 10">` +
		`<svg:path d="M0 0h10v10z"/></svg:svg>`
	cases := []struct{ name, svg, want string }{
		// A clean logo, and the edges that must stay clean.
		{"a single path", cleanLogo, ""},
		{"a root and a path with a prefix", prefixed, ""},
		{"an xml declaration and comments, one holding a script tag",
			`<?xml version="1.0" encoding="UTF-8"?>` + "\n<!-- exported -->\n" +
				inLogo(logoShape+`<!-- <script>alert(1)</script> -->`), ""},
		{"hrefs that point to a fragment",
			inLogo(`<defs><path id="a" d="M0 0h10v10z"/></defs><use href="#a"/><use xlink:href="#a"/>`), ""},
		{"a gradient named in an attribute",
			inLogo(`<defs><linearGradient id="g"/></defs><path fill="url(#g)" d="M0 0h10v10z"/>`), ""},
		{"a style without an import or a url", inLogo(`<style>.a{fill:#fff}</style>` + logoShape), ""},
		{"attributes that only start with o", inLogo(`<path opacity="0.5" overflow="hidden" d="M0 0h10v10z"/>`), ""},
		{"rule words in a title and an id",
			inLogo(`<title>@import url( script</title><path id="onload" d="M0 0h10v10z"/>`), ""},
		{"a style that only looks like an import or a url",
			inLogo(`<style>.curl{fill:#fff !important}</style>` + logoShape), ""},
		{"two styles whose texts only add up to an import", inLogo(`<style>@imp</style><style>ort</style>`), ""},
		{"text before a style that only adds up to an import", inLogo(`<title>@imp</title><style>ort</style>`), ""},

		{"a logo over 50 KB", paddedLogo(51201), "over 50 KB"},

		{"text that is not XML", "this is not a logo", "not XML"},
		{"an empty file", "", "not XML"},
		{"a truncated file", `<svg viewBox="0 0 10 10"><path d="M0 0`, "not XML"},
		{"an element left open", `<svg viewBox="0 0 10 10"><path/>`, "not XML"},
		{"an end tag that does not match", `<svg viewBox="0 0 10 10"></g>`, "not XML"},
		{"an entity nobody defined", inLogo(`&nbsp;`), "not XML"},
		{"two roots", cleanLogo + cleanLogo, "not XML"},

		{"an html root", `<html xmlns="http://www.w3.org/1999/xhtml"><body/></html>`, "root is not svg"},
		{"a group as the root", `<g viewBox="0 0 10 10"><path d="M0 0h10v10z"/></g>`, "root is not svg"},
		{"a root in capitals", `<SVG viewBox="0 0 10 10"><path d="M0 0h10v10z"/></SVG>`, "root is not svg"},

		{"an svg with width and height only",
			`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><path d="M0 0h10v10z"/></svg>`, "no viewBox"},
		{"a viewBox spelled in lowercase", `<svg viewbox="0 0 10 10"><path d="M0 0h10v10z"/></svg>`, "no viewBox"},
		{"a viewBox on an inner svg only", `<svg><svg viewBox="0 0 10 10"/></svg>`, "no viewBox"},

		{"a doctype before the root",
			`<!DOCTYPE svg PUBLIC "-//W3C//DTD SVG 1.1//EN" "http://www.w3.org/Graphics/SVG/1.1/DTD/svg11.dtd">` +
				"\n" + cleanLogo, "doctype"},
		{"a doctype that defines an entity", `<!DOCTYPE svg [<!ENTITY x "y">]>` + cleanLogo, "doctype"},
		{"a doctype inside the root", inLogo(`<!DOCTYPE svg>` + logoShape), "doctype"},
		{"a doctype after the root", cleanLogo + `<!DOCTYPE svg>`, "doctype"},

		{"a script", inLogo(`<script>alert(1)</script>`), "script"},
		{"two scripts, which are one problem", inLogo(`<script/><script/>`), "script"},
		{"a script in capitals", inLogo(`<SCRIPT>alert(1)</SCRIPT>`), "script"},
		{"a script with a prefix", inLogo(`<svg:script xmlns:svg="http://www.w3.org/2000/svg">alert(1)</svg:script>`),
			"script"},
		{"a foreignObject",
			inLogo(`<foreignObject width="10" height="10"><div xmlns="http://www.w3.org/1999/xhtml">hi</div></foreignObject>`),
			"foreignobject"},
		{"an image", inLogo(`<image width="10" height="10"/>`), "image"},
		{"an iframe", inLogo(`<iframe width="10" height="10"/>`), "iframe"},

		{"a style that imports", inLogo(`<style>@import "https://example.com/a.css";</style>`), "style import"},
		{"a style that imports, in CDATA", inLogo(`<style><![CDATA[@import "https://example.com/a.css";]]></style>`),
			"style import"},
		{"an import in capitals", inLogo(`<style>@IMPORT "a.css";</style>`), "style import"},
		{"an import split by a comment", inLogo(`<style>@imp<!-- split -->ort "a.css";</style>`), "style import"},
		{"an import split between text and CDATA", inLogo(`<style>@im<![CDATA[port]]> "a.css";</style>`),
			"style import"},
		{"an import in a style with a prefix",
			inLogo(`<svg:style xmlns:svg="http://www.w3.org/2000/svg">@import "a.css";</svg:style>`), "style import"},
		{"an import in the first of two styles",
			inLogo(`<style>@import "a.css";</style><style>.a{fill:#fff}</style>`), "style import"},
		{"an import in a style in capitals", inLogo(`<STYLE>@import "a.css";</STYLE>`), "style import"},
		{"an import before a style inside the style", inLogo(`<style>@import "a.css";<style/>x</style>`), "style import"},
		{"an import after a style inside the style", inLogo(`<style>x<style/>@import "a.css";</style>`), "style import"},

		{"a style with a url", inLogo(`<style>.a{fill:url(https://example.com/a.svg)}</style>`), "style url"},
		{"a style with a url, in CDATA", inLogo(`<style><![CDATA[a{background:url(x)}]]></style>`), "style url"},
		{"a url in capitals, even to a fragment", inLogo(`<style>.a{fill:URL(#g)}</style>`), "style url"},

		{"onload on the root",
			`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10" onload="alert(1)">` + logoShape + `</svg>`,
			"event attribute"},
		{"onclick on an inner element", inLogo(`<path onclick="alert(1)" d="M0 0h10v10z"/>`), "event attribute"},
		{"an event attribute in capitals", inLogo(`<path ONMOUSEOVER="alert(1)" d="M0 0h10v10z"/>`), "event attribute"},

		{"an https href", inLogo(`<a href="https://example.com">` + logoShape + `</a>`), "external href"},
		{"a javascript href", inLogo(`<a href="javascript:alert(1)">` + logoShape + `</a>`), "external href"},
		{"a data xlink:href on use", inLogo(`<use xlink:href="data:image/svg+xml;base64,PHN2Zy8+"/>`), "external href"},
		{"a relative href", inLogo(`<use href="other.svg#a"/>`), "external href"},
		{"an empty href", inLogo(`<use href=""/>`), "external href"},
		{"an href with another prefix",
			inLogo(`<use xmlns:x="http://www.w3.org/1999/xlink" x:href="https://example.com/a.svg#a"/>`), "external href"},
		{"an href in capitals", inLogo(`<use HREF="https://example.com/a.svg#a"/>`), "external href"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := checkLogo("example.svg", []byte(c.svg))
			switch {
			case c.want == "" && len(got) != 0:
				t.Errorf("want no problem, got %q", got)
			case c.want != "" && (len(got) != 1 || !strings.Contains(got[0], c.want)):
				t.Errorf("want exactly one problem containing %q, got %q", c.want, got)
			}
		})
	}
}

// TestCheckLogoSizeLimitIsFiftyKB: 50 KB is 51,200 bytes, a KB being 1024 bytes as in the site's own caps. The file at
// the limit passes and one byte more does not.
func TestCheckLogoSizeLimitIsFiftyKB(t *testing.T) {
	for _, c := range []struct {
		size int
		over bool
	}{{51199, false}, {51200, false}, {51201, true}} {
		svg := paddedLogo(c.size)
		if len(svg) != c.size {
			t.Fatalf("the fixture is %d bytes long, want %d", len(svg), c.size)
		}
		got := checkLogo("big.svg", []byte(svg))
		switch {
		case c.over && (len(got) != 1 || !strings.Contains(got[0], "over 50 KB")):
			t.Errorf("a logo of %d bytes gave %q, want one problem: over 50 KB", c.size, got)
		case !c.over && len(got) != 0:
			t.Errorf("a logo of %d bytes gave %q, want none", c.size, got)
		}
	}
}

// TestCheckLogoReportsEveryProblemAtOnce: a logo that breaks several rules gets a message for each, in the order of the
// rules and each naming the file; a DOCTYPE that defines an entity the file then uses is a doctype and not XML; a root
// that is not svg is not also asked for a viewBox.
func TestCheckLogoReportsEveryProblemAtOnce(t *testing.T) {
	// The svg is left open, so that the walk fails at the end of the file, after it has seen everything above.
	everything := "<!--" + strings.Repeat(" ", 51200) + "-->" + `<!DOCTYPE svg><svg onload="x">` +
		`<script/><foreignObject/><image href="https://example.com/a.png"/><iframe/>` +
		`<style>@import "a.css";.b{fill:url(#g)}</style>`
	cases := []struct {
		name, svg string
		want      []string // the key of each message, in order
	}{
		{"every rule at once", everything, []string{"over 50 KB", "not XML", "no viewBox", "doctype", "script",
			"foreignobject", "image", "iframe", "style import", "style url", "event attribute", "external href"}},
		{"a doctype with an entity the file uses", `<!DOCTYPE svg [<!ENTITY x "y">]><svg viewBox="0 0 10 10">&x;</svg>`,
			[]string{"not XML", "doctype"}},
		{"a root that is not svg beside a script", `<html><script/></html>`, []string{"root is not svg", "script"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := checkLogo("bad.svg", []byte(c.svg))
			if len(got) != len(c.want) {
				t.Fatalf("got %d problems, want %d: %q", len(got), len(c.want), got)
			}
			for i, key := range c.want {
				if !strings.HasPrefix(got[i], `logo "bad.svg": `) || !strings.Contains(got[i], key) {
					t.Errorf("problem %d = %q, want it to name the file and carry %q", i, got[i], key)
				}
			}
		})
	}
}

// TestCheckLogoSaysWhatItFound: a message names what a contributor has to change: the reason a file is not XML, the
// root that is not svg, the event attribute and the element it sits on, the element and the value of an external href.
// A long value, such as an embedded picture, is cut short.
func TestCheckLogoSaysWhatItFound(t *testing.T) {
	cases := []struct {
		name, svg string
		want      []string // fragments the one message holds
	}{
		{"a file that is not XML", `<svg viewBox="0 0 10 10"><path d="M0 0`, []string{"not XML", "unexpected EOF"}},
		{"a root that is not svg", `<html/>`, []string{"<html>"}},
		{"an event attribute", inLogo(`<path onclick="x" d="M0 0h10v10z"/>`), []string{"onclick", "<path>"}},
		{"an external href", inLogo(`<use xlink:href="https://example.com/a.svg#a"/>`),
			[]string{"<use>", "https://example.com/a.svg#a"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := checkLogo("x.svg", []byte(c.svg))
			if len(got) != 1 {
				t.Fatalf("got %d problems, want 1: %q", len(got), got)
			}
			for _, fragment := range c.want {
				if !strings.Contains(got[0], fragment) {
					t.Errorf("problem %q does not hold %q", got[0], fragment)
				}
			}
		})
	}
	picture := inLogo(`<use xlink:href="data:image/png;base64,` + strings.Repeat("A", 5000) + `"/>`)
	got := checkLogo("x.svg", []byte(picture))
	if len(got) != 1 {
		t.Fatalf("an embedded picture gave %d problems, want 1: %q", len(got), got)
	}
	if len(got[0]) > 200 {
		t.Errorf("the problem is %d bytes long, want it cut short (under 200 bytes)", len(got[0]))
	}
}

// TestCheckLogoOwnersFindsTheOrphan: a logo belongs to a sponsor of sponsors.json whose tier is shown with a logo (a
// Title, Lab sponsor or Team sponsor); a logo of an unknown id, of a Lab hours sponsor or of a Supporter is an orphan.
// A sponsor whose credit has ended keeps its logo in the folder.
func TestCheckLogoOwnersFindsTheOrphan(t *testing.T) {
	cases := []struct {
		name  string
		ids   []string
		ended bool     // the Team sponsor's last month has passed
		want  []string // a fragment of each orphan's message, in the order of the ids
	}{
		{name: "no logo at all"},
		{name: "the logos of the three tiers that take one", ids: []string{"title-co", "lab-co", "team-co"}},
		{name: "a logo of a sponsor whose credit has ended", ids: []string{"team-co"}, ended: true},
		{name: "a logo that belongs to nobody", ids: []string{"stranger-co"}, want: []string{`no sponsor "stranger-co"`}},
		{name: "a logo of a Lab hours sponsor", ids: []string{"hours-co"}, want: []string{`"hours-co" is lab-hours`}},
		{name: "a logo of a Supporter", ids: []string{"board-co"}, want: []string{`"board-co" is supporter`}},
		{name: "an orphan among good logos", ids: []string{"title-co", "stranger-co", "team-co"},
			want: []string{`no sponsor "stranger-co"`}},
		{name: "two orphans", ids: []string{"hours-co", "title-co", "stranger-co"},
			want: []string{`"hours-co" is lab-hours`, `no sponsor "stranger-co"`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sponsors := cleanDoc().Sponsors
			if c.ended {
				sponsors[teamCo].Since, sponsors[teamCo].Until = "2025-01", strPtr("2025-12")
			}
			got := checkLogoOwners(c.ids, sponsors)
			if len(got) != len(c.want) {
				t.Fatalf("got %d problems, want %d: %q", len(got), len(c.want), got)
			}
			for i, fragment := range c.want {
				if !strings.Contains(got[i], "orphan logo") || !strings.Contains(got[i], fragment) {
					t.Errorf("problem %d = %q, want an orphan logo with %q", i, got[i], fragment)
				}
			}
		})
	}
}

// TestCommittedLogosFollowTheRules: every logo in docs/evals/sponsors passes the logo rules and belongs to a sponsor of
// the committed sponsors.json whose tier is shown with a logo, so that a logo that breaks a rule, or that nobody
// credits, fails in CI before the site reads it. The folder does not exist until the first logo.
func TestCommittedLogosFollowTheRules(t *testing.T) {
	var doc sponsorsDoc
	readJSON(t, sponsorsPath, &doc)
	files, err := filepath.Glob(logosGlob)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(file)
		for _, problem := range checkLogo(name, data) {
			t.Error(problem)
		}
		ids = append(ids, strings.TrimSuffix(name, ".svg"))
	}
	for _, problem := range checkLogoOwners(ids, doc.Sponsors) {
		t.Error(problem)
	}
}
