package docscheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// logoOpen starts a clean logo: one root svg with a viewBox and the two namespaces a logo may use.
const logoOpen = `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" ` +
	`viewBox="0 0 10 10">`

// logoShape is the single path of a clean logo.
const logoShape = `<path d="M0 0h10v10H0z"/>`

// inLogo wraps a body in a clean root, so that a case shows only what it adds.
func inLogo(body string) string { return logoOpen + body + "</svg>" }

// cleanLogo follows every rule: one svg with a viewBox, holding one path.
var cleanLogo = inLogo(logoShape)

// rootWith is a root svg in the SVG namespace with the attributes given, holding body.
func rootWith(attrs, body string) string {
	return `<svg xmlns="` + svgNamespace + `" ` + attrs + `>` + body + `</svg>`
}

// declaration is the XML declaration that an editor writes at the top of a file.
const declaration = `<?xml version="1.0" encoding="UTF-8"?>`

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
			declaration + "\n<!-- exported -->\n" + inLogo(logoShape+`<!-- <script>alert(1)</script> -->`), ""},
		{"a declaration in single quotes, with spaces and a standalone",
			`<?xml version = '1.0' encoding='utf-8' standalone='no' ?>` + cleanLogo, ""},
		{"a byte order mark before the declaration", utf8BOM + declaration + cleanLogo, ""},
		{"a byte order mark before the root", utf8BOM + cleanLogo, ""},
		{"blank lines around the root", "\n\t \r\n" + cleanLogo + "\n\n", ""},
		{"a comment after the root", cleanLogo + "\n<!-- end -->\n", ""},
		{"an href and an xlink:href on one element", inLogo(`<use href="#a" xlink:href="#a"/>`), ""},
		{"text inside the root", inLogo("Example Co" + logoShape), ""},
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
		{"a truncated file", logoOpen + `<path d="M0 0`, "not XML"},
		{"an element left open", logoOpen + `<path/>`, "not XML"},
		{"an end tag that does not match", logoOpen + `</g>`, "not XML"},
		{"an entity nobody defined", inLogo(`&nbsp;`), "not XML"},
		{"two roots", cleanLogo + cleanLogo, "not XML"},
		{"an encoding other than UTF-8", `<?xml version="1.0" encoding="ISO-8859-1"?>` + cleanLogo,
			"save the file as UTF-8"},

		{"text before the root", "logo" + cleanLogo, "text before the root"},
		{"two byte order marks", utf8BOM + utf8BOM + cleanLogo, "text before the root"},
		{"a no-break space before the root, which is no XML white space", string(rune(0xa0)) + cleanLogo,
			"text before the root"},
		{"a CDATA section before the root", `<![CDATA[ ]]>` + cleanLogo, "text before the root"},
		{"a character reference before the root", `&#32;` + cleanLogo, "text before the root"},
		{"text after the root", cleanLogo + "logo", "text after the root"},
		{"a CDATA section after the root", cleanLogo + `<![CDATA[ ]]>`, "text after the root"},

		{"a blank line before the declaration", "\n" + declaration + cleanLogo, "declaration out of place"},
		{"a comment before the declaration", "<!-- exported -->" + declaration + cleanLogo, "declaration out of place"},
		{"a second declaration", declaration + declaration + cleanLogo, "declaration out of place"},
		{"a declaration inside the root", inLogo(declaration + logoShape), "declaration out of place"},
		{"a declaration after the root", cleanLogo + declaration, "declaration out of place"},
		{"a declaration without a version", `<?xml encoding="UTF-8"?>` + cleanLogo, "malformed xml declaration"},
		{"a declaration with nothing in it", `<?xml?>` + cleanLogo, "malformed xml declaration"},
		{"a version other than 1.0, spaced so the decoder does not read it", `<?xml version = "1.1"?>` + cleanLogo,
			"malformed xml declaration"},
		{"a standalone that is neither yes nor no", `<?xml version="1.0" standalone="maybe"?>` + cleanLogo,
			"malformed xml declaration"},
		{"the encoding after the standalone", `<?xml version="1.0" standalone="yes" encoding="UTF-8"?>` + cleanLogo,
			"malformed xml declaration"},

		{"a stylesheet instruction", `<?xml-stylesheet href="a.css"?>` + cleanLogo, "processing instruction"},
		{"an instruction inside the root", inLogo(`<?php echo 1; ?>` + logoShape), "processing instruction"},
		{"a declaration in capitals", `<?XML version="1.0"?>` + cleanLogo, "processing instruction"},

		{"an attribute twice", inLogo(`<path d="M0 0h10v10z" d="M0 0h5v5z"/>`), "duplicate attribute"},
		{"an attribute twice under two prefixes of one namespace",
			inLogo(`<use xmlns:x="http://www.w3.org/1999/xlink" xlink:href="#a" x:href="#a"/>`), "duplicate attribute"},
		{"a viewBox twice on the root", rootWith(`viewBox="0 0 10 10" viewBox="0 0 10 10"`, logoShape),
			"duplicate attribute"},

		{"an html root", `<html xmlns="http://www.w3.org/1999/xhtml"><body/></html>`, "root is not svg"},
		{"a group as the root", `<g viewBox="0 0 10 10"><path d="M0 0h10v10z"/></g>`, "root is not svg"},
		{"a root in capitals", `<SVG viewBox="0 0 10 10"><path d="M0 0h10v10z"/></SVG>`, "root is not svg"},

		{"a root without xmlns", `<svg viewBox="0 0 10 10">` + logoShape + `</svg>`, "not in the SVG namespace"},
		{"a root with an unbound svg: prefix", `<svg:svg viewBox="0 0 10 10"><svg:path d="M0 0h10v10z"/></svg:svg>`,
			"not in the SVG namespace"},
		{"a root in the XHTML namespace", `<svg xmlns="http://www.w3.org/1999/xhtml" viewBox="0 0 10 10">` + logoShape +
			`</svg>`, "not in the SVG namespace"},

		{"an svg with width and height only", rootWith(`width="10" height="10"`, logoShape), "no viewBox"},
		{"a viewBox spelled in lowercase", rootWith(`viewbox="0 0 10 10"`, logoShape), "no viewBox"},
		{"a viewBox on an inner svg only", rootWith("", `<svg viewBox="0 0 10 10"/>`), "no viewBox"},
		{"a viewBox with a prefix", rootWith(`xmlns:x="urn:x" x:viewBox="0 0 10 10"`, logoShape), "no viewBox"},
		{"an empty viewBox", rootWith(`viewBox=""`, logoShape), "empty viewBox"},
		{"a viewBox of spaces", rootWith(`viewBox="  "`, logoShape), "empty viewBox"},

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

		{"an import spelled with a CSS escape", inLogo(`<style>@` + backslash + `69mport "a.css";</style>`),
			"style escape"},
		{"a url spelled with a CSS escape", inLogo(`<style>.a{fill:u` + backslash + `72l(x)}</style>`), "style escape"},
		{"a CSS escape in CDATA", inLogo(`<style><![CDATA[.a::after{content:"` + backslash + `2014"}]]></style>`),
			"style escape"},

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
// the limit passes and one byte more does not, a byte order mark counted: the site weighs the file as it is.
func TestCheckLogoSizeLimitIsFiftyKB(t *testing.T) {
	for _, c := range []struct {
		size int
		bom  bool // the file opens with a byte order mark, which counts toward its size
		over bool
	}{{51199, false, false}, {51200, false, false}, {51201, false, true}, {51200, true, false}, {51201, true, true}} {
		svg := paddedLogo(c.size)
		if c.bom {
			svg = utf8BOM + paddedLogo(c.size-len(utf8BOM))
		}
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
// that is not svg is not also asked for a namespace or a viewBox.
func TestCheckLogoReportsEveryProblemAtOnce(t *testing.T) {
	// The file opens with a declaration without a version and holds a second one out of place; the svg closes, text
	// follows it, and a second svg is left open, so that the walk fails at the end of the file after it has seen
	// everything above.
	everything := `<?xml encoding="UTF-8"?><!--` + strings.Repeat(" ", 51200) + `-->` + declaration +
		`<?xml-stylesheet href="a.css"?><!DOCTYPE svg>x<svg onload="x" a="1" a="2">` +
		`<script/><foreignObject/><image href="https://example.com/a.png"/><iframe/>` +
		`<style>@import "a.css";.b{fill:url(#g)}.c{content:"` + backslash + `"}</style></svg>y<svg`
	cases := []struct {
		name, svg string
		want      []string // the key of each message, in order
	}{
		{"every rule at once", everything, []string{"over 50 KB", "not XML", "text before the root",
			"text after the root", "declaration out of place", "malformed xml declaration", "processing instruction",
			"duplicate attribute", "not in the SVG namespace", "no viewBox", "doctype", "script", "foreignobject",
			"image", "iframe", "style import", "style url", "style escape", "event attribute", "external href"}},
		{"a doctype with an entity the file uses",
			`<!DOCTYPE svg [<!ENTITY x "y">]>` + rootWith(`viewBox="0 0 10 10"`, "&x;"), []string{"not XML", "doctype"}},
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

// TestCheckLogoSaysWhatItFound: a message names what a contributor has to change: the reason a file is not XML (and,
// for another encoding, to save it as UTF-8), the text before the root, the malformed declaration, the processing
// instruction, the attribute given twice and its element, the namespace the root needs, the root that is not svg, the
// event attribute and the element it sits on, the element and the value of an external href. A long value, such as an
// embedded picture, is cut short.
func TestCheckLogoSaysWhatItFound(t *testing.T) {
	cases := []struct {
		name, svg string
		want      []string // fragments the one message holds
	}{
		{"a file that is not XML", logoOpen + `<path d="M0 0`, []string{"not XML", "unexpected EOF"}},
		{"an encoding other than UTF-8", `<?xml version="1.0" encoding="ISO-8859-1"?>` + cleanLogo,
			[]string{"not XML", "ISO-8859-1", "save the file as UTF-8"}},
		{"text before the root", "logo<!-- -->more" + cleanLogo, []string{`text before the root: "logo"`}},
		{"a malformed declaration", `<?xml encoding="UTF-8"?>` + cleanLogo, []string{`"<?xml encoding=\"UTF-8\"?>"`}},
		{"a processing instruction", `<?xml-stylesheet href="a.css"?>` + cleanLogo, []string{"<?xml-stylesheet?>"}},
		{"an attribute twice", inLogo(`<path d="M0 0h10v10z" d="M0 0h5v5z"/>`),
			[]string{"duplicate attribute d on <path>"}},
		{"a root outside the SVG namespace", `<svg viewBox="0 0 10 10"/>`, []string{`xmlns="` + svgNamespace + `"`}},
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
