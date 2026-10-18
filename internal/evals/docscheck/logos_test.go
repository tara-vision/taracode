package docscheck

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"slices"
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

// nested is a logo whose deepest element, a path, stands at this level (the root svg is level 1), under groups.
func nested(levels int) string {
	groups := levels - 2
	return inLogo(strings.Repeat("<g>", groups) + logoShape + strings.Repeat("</g>", groups))
}

// nameOf is a name of exactly size bytes that opens with head and goes on with the letter a.
func nameOf(head string, size int) string { return head + strings.Repeat("a", size-len(head)) }

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
		{"accented letters in a comment and a title", "<!-- caf" + string(rune(0xe9)) + " -->" +
			inLogo("<title>caf"+string(rune(0xe9))+"</title>"+logoShape), ""},
		{"the replacement character and a character beyond U+FFFF in a title",
			inLogo("<title>" + string(rune(0xfffd)) + string(rune(0x1f600)) + "</title>" + logoShape), ""},
		{"a prefix bound on its own element", inLogo(`<x:path xmlns:x="urn:x" d="M0 0h10v10z"/>`), ""},
		{"an xml: attribute, whose prefix is always bound", inLogo(`<path xml:space="preserve" d="M0 0h10v10z"/>`), ""},
		{"the default namespace taken back, which XML allows", inLogo(`<g xmlns="">` + logoShape + `</g>`), ""},
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
		{"character references next to the surrogates", inLogo(`<title>&#xD7FF;&#xE000;</title>` + logoShape), ""},
		{"character references next to the surrogates, in decimal", inLogo(`<path d="&#55295;&#57344;"/>`), ""},
		{"an escaped ampersand before what reads as a reference to a surrogate",
			inLogo(`<title>&amp;#xD800;</title>` + logoShape), ""},
		{"attributes separated by a space", inLogo(`<path d="M0 0" fill="red"/>`), ""},
		{"attributes separated by a tab, a line feed and a carriage return",
			inLogo("<path d=\"M0 0\"\t\n\rfill=\"red\"/>"), ""},
		{"attributes in single quotes and in double quotes", inLogo(`<path d='M0 0' fill="red" id='a'/>`), ""},
		{"values that hold quotes, an equals sign and the end of a tag",
			inLogo(`<path id='a"b="1"' d="M0 0/>" fill='x>y'/>`), ""},
		{"white space around the equals sign", inLogo("<path d =\t\"M0 0\"\nfill\r= 'red' />"), ""},
		{"prefixed names whose local parts start with a letter or an underscore",
			inLogo(`<a:b1 xmlns:a="urn:a" xmlns:b1="urn:b" xmlns:_c="urn:c" a:d.x="1" a:_y="2"/>`), ""},
		{"a local part that starts with a letter beyond ASCII",
			inLogo(`<a:` + string(rune(0xe9)) + ` xmlns:a="urn:a"/>`), ""},
		{"a prefix bound to a namespace spelled like it", inLogo(`<g xmlns:x="x"><x:path d="M0 0h10v10z"/></g>`), ""},
		{"a prefix bound on a parent and used by a child and its attribute",
			inLogo(`<g xmlns:x="urn:x"><x:path x:d="1"/></g>`), ""},
		{"the xml prefix declared for the xml namespace",
			inLogo(`<g xmlns:xml="` + xmlNamespace + `" xml:space="preserve"/>`), ""},
		{"a prefix declared for a namespace that only starts like the xml namespace",
			inLogo(`<g xmlns:p="` + xmlNamespace + `/"><p:path d="M0 0h10v10z"/></g>`), ""},
		{"nesting 256 levels deep", nested(256), ""},
		{"many elements side by side", inLogo(strings.Repeat("<g/>", 300)), ""},
		{"an element name of 256 bytes", inLogo("<" + nameOf("", 256) + "/>"), ""},
		{"an attribute name of 256 bytes", inLogo("<path " + nameOf("", 256) + `="1"/>`), ""},
		{"a prefixed element name of 256 bytes", inLogo("<" + nameOf("p:", 256) + ` xmlns:p="urn:p"/>`), ""},
		{"a prefixed attribute name of 256 bytes", inLogo(`<path xmlns:p="urn:p" ` + nameOf("p:", 256) + `="1"/>`), ""},
		{"a namespace declaration of 256 bytes", inLogo("<g " + nameOf("xmlns:", 256) + `="urn:a"/>`), ""},
		{"an element name of 128 two-byte characters, 256 bytes",
			inLogo("<" + strings.Repeat(string(rune(0xe9)), 128) + "/>"), ""},

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
		{"a Latin-1 byte in a comment", "<!-- " + string([]byte{0xa9}) + " Example Co -->" + cleanLogo,
			"save the file as UTF-8"},
		{"a control character in a comment", "<!-- " + string(rune(0x1)) + " -->" + cleanLogo, "XML does not allow"},
		{"U+FFFF in a comment", "<!--" + string(rune(0xffff)) + "-->" + cleanLogo, "XML does not allow"},
		{"a control character in a title", inLogo("<title>" + string(rune(0x1)) + "</title>"), "XML does not allow"},

		{"a character reference to a surrogate", inLogo(`<title>&#xD800;</title>` + logoShape), "character reference"},
		{"a character reference to the last surrogate, in decimal, in an attribute", inLogo(`<path d="&#57343;"/>`),
			"character reference"},
		{"a character reference to the first surrogate, in decimal", inLogo(`<title>&#55296;</title>`),
			"character reference"},
		{"a character reference to a surrogate with leading zeros", inLogo(`<title>&#x0000DBFF;</title>`),
			"character reference"},
		{"a character reference to a surrogate, in an attribute in single quotes", inLogo(`<path d='&#xDC00;'/>`),
			"character reference"},
		{"a character reference to a surrogate, in a comment", inLogo(`<!-- &#xD800; -->` + logoShape),
			"character reference"},
		{"a character reference to a surrogate, in CDATA", inLogo(`<title><![CDATA[&#xDFFF;]]></title>`),
			"character reference"},

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

		{"attributes with no white space between them",
			`<svg xmlns="` + svgNamespace + `"viewBox="0 0 10 10">` + logoShape + `</svg>`, "white space"},
		{"single-quoted attributes with no white space between them",
			`<svg xmlns='` + svgNamespace + `'viewBox='0 0 10 10'>` + logoShape + `</svg>`, "white space"},
		{"attributes with no white space between them, on a child", inLogo(`<path d="M0 0"fill="red"/>`), "white space"},
		{"attributes with no white space after a value that holds the end of a tag", inLogo(`<path d="M0 0/>"fill="red"/>`),
			"white space"},

		{"an element with an unbound prefix", inLogo(`<x:path d="M0 0h10v10z"/>`), "unbound prefix"},
		{"an xlink:href without xmlns:xlink", rootWith(`viewBox="0 0 10 10"`, `<use xlink:href="#a"/>`),
			"unbound prefix"},
		{"a prefix bound only on a sibling", inLogo(`<g xmlns:x="urn:x"/><x:path d="M0 0h10v10z"/>`), "unbound prefix"},
		{"a prefix that names a namespace declared only on a sibling",
			inLogo(`<g xmlns:y="x"/><x:path d="M0 0h10v10z"/>`), "unbound prefix"},
		{"an unbound prefix spelled like a namespace declared in scope",
			inLogo(`<g xmlns:a="x"><x:path d="M0 0h10v10z"/></g>`), "unbound prefix"},
		{"an unbound attribute prefix spelled like a namespace declared in scope", inLogo(`<g xmlns:a="x" x:b="1"/>`),
			"unbound prefix"},
		{"an unbound prefix spelled like the default namespace in scope",
			inLogo(`<g xmlns="x"><x:path d="M0 0h10v10z"/></g>`), "unbound prefix"},
		{"xlink: spelled like a namespace that another prefix is bound to",
			rootWith(`viewBox="0 0 10 10" xmlns:a="xlink"`, `<use xlink:href="#a"/>`), "unbound prefix"},
		{"a prefix declared for the xml namespace, used by an element",
			inLogo(`<g xmlns:p="` + xmlNamespace + `"><p:path d="M0 0h10v10z"/></g>`), "unbound prefix"},
		{"a prefix declared for the xml namespace, used by an attribute",
			inLogo(`<g xmlns:p="` + xmlNamespace + `" p:a="1"/>`), "unbound prefix"},
		{"a prefix declared for the xml namespace spelled with a character reference",
			inLogo(`<g xmlns:p="http&#58;//www.w3.org/XML/1998/namespace"><p:path d="M0 0h10v10z"/></g>`), "unbound prefix"},
		{"a prefix declared for the xmlns namespace, used by an element",
			inLogo(`<g xmlns:p="` + xmlnsNamespace + `"><p:path d="M0 0h10v10z"/></g>`), "unbound prefix"},
		{"a prefix declared for the xmlns namespace, used by an attribute",
			inLogo(`<g xmlns:p="` + xmlnsNamespace + `" p:a="1"/>`), "unbound prefix"},
		{"the xmlns prefix declared, used by an element", inLogo(`<g xmlns:xmlns="urn:x"><xmlns:path d="M0 0h10v10z"/></g>`),
			"unbound prefix"},
		{"a prefix declared with an empty namespace", inLogo(`<g xmlns:x=""><x:path d="M0 0h10v10z"/></g>`),
			"empty namespace"},
		{"an element name that opens with a colon", inLogo(`<:path d="M0 0h10v10z"/>`), "malformed name"},
		{"an element name that ends with a colon", inLogo(`<path:/>`), "malformed name"},
		{"an attribute name that opens with a colon", inLogo(`<path :d="M0 0h10v10z"/>`), "malformed name"},
		{"a prefixed element name whose local part starts with a digit", inLogo(`<a:1b xmlns:a="urn:a"/>`),
			"malformed name"},
		{"a prefixed attribute name whose local part starts with a dot", inLogo(`<path xmlns:a="urn:a" a:.d="x"/>`),
			"malformed name"},
		{"a prefixed element name whose local part starts with a hyphen", inLogo(`<a:-b xmlns:a="urn:a"/>`),
			"malformed name"},
		{"a namespace declaration whose prefix starts with a digit", inLogo(`<g xmlns:6link="urn:a"/>`), "malformed name"},

		{"nesting 257 levels deep", nested(257), "nesting"},
		{"nesting 7000 levels deep", nested(7000), "nesting"},

		{"an element name of 257 bytes", inLogo("<" + nameOf("", 257) + "/>"), "name longer"},
		{"an attribute name of 257 bytes", inLogo("<path " + nameOf("", 257) + `="1"/>`), "name longer"},
		{"a prefixed element name of 257 bytes", inLogo("<" + nameOf("p:", 257) + ` xmlns:p="urn:p"/>`), "name longer"},
		{"a prefixed attribute name of 257 bytes", inLogo(`<path xmlns:p="urn:p" ` + nameOf("p:", 257) + `="1"/>`),
			"name longer"},
		{"a namespace declaration of 257 bytes", inLogo("<g " + nameOf("xmlns:", 257) + `="urn:a"/>`), "name longer"},
		{"an element name of 129 two-byte characters, 258 bytes",
			inLogo("<" + strings.Repeat(string(rune(0xe9)), 129) + "/>"), "name longer"},
		{"an element name of 50001 bytes", inLogo("<" + nameOf("", 50001) + "/>"), "name longer"},
		{"an attribute name of 50001 bytes", inLogo("<path " + nameOf("", 50001) + `="1"/>`), "name longer"},

		{"an html root", `<html xmlns="http://www.w3.org/1999/xhtml"><body/></html>`, "root is not svg"},
		{"a group as the root", `<g viewBox="0 0 10 10"><path d="M0 0h10v10z"/></g>`, "root is not svg"},
		{"a root in capitals", `<SVG viewBox="0 0 10 10"><path d="M0 0h10v10z"/></SVG>`, "root is not svg"},

		{"a root without xmlns", `<svg viewBox="0 0 10 10">` + logoShape + `</svg>`, "not in the SVG namespace"},
		{"a root with an unbound svg: prefix", `<svg:svg viewBox="0 0 10 10">` + logoShape + `</svg:svg>`,
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
		`<x:g/><g:/><title>&#xD800;</title><g id="a"class="b"/><g xmlns:p="urn:p"><p:1b/></g>` +
		strings.Repeat("<g>", 256) + strings.Repeat("</g>", 256) + "<" + nameOf("", 257) + "/>" +
		`<script/><foreignObject/><image href="https://example.com/a.png"/><iframe/>` +
		`<style>@import "a.css";.b{fill:url(#g)}.c{content:"` + backslash + `"}</style></svg>y<svg`
	cases := []struct {
		name, svg string
		want      []string // the key of each message, in order
	}{
		{"every rule at once", everything, []string{"over 50 KB", "not XML", "character reference",
			"text before the root", "text after the root", "declaration out of place", "malformed xml declaration",
			"processing instruction", "white space", "duplicate attribute", "unbound prefix", "malformed name",
			"malformed name", "name longer", "nesting", "not in the SVG namespace", "no viewBox", "doctype", "script",
			"foreignobject", "image", "iframe", "style import", "style url", "style escape", "event attribute",
			"external href"}},
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
// instruction, the attribute given twice and its element, the prefix that nothing declares, the character reference to
// a surrogate, the attribute that follows no white space, the name that does not start as a name, the depth or the
// length that passes the limit, the namespace the root needs, the root that is not svg, the event attribute and the
// element it sits on, the element and the value of an external href. A long value, such as an embedded picture, is cut
// short.
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
		{"a byte that is not UTF-8", "<!-- " + string([]byte{0xa9}) + " -->" + cleanLogo,
			[]string{"not XML", "at byte 5", "save the file as UTF-8"}},
		{"a character XML does not allow", "<!--" + string(rune(0x1)) + "-->" + cleanLogo,
			[]string{"not XML", "U+0001", "at byte 4"}},
		{"a character XML does not allow, after a byte order mark", utf8BOM + "<!--" + string(rune(0x1)) + "-->" +
			cleanLogo, []string{"U+0001", "at byte 7"}},
		{"an element with an unbound prefix", inLogo(`<x:path d="M0 0h10v10z"/>`),
			[]string{"unbound prefix x on <path>", "xmlns:x"}},
		{"an attribute with an unbound prefix", rootWith(`viewBox="0 0 10 10"`, `<use xlink:href="#a"/>`),
			[]string{"unbound prefix xlink on the attribute href of <use>", "xmlns:xlink"}},
		{"an unbound prefix spelled like a namespace in scope", inLogo(`<g xmlns:a="x"><x:path d="M0 0h10v10z"/></g>`),
			[]string{"unbound prefix x on <path>", "xmlns:x"}},
		{"a prefix declared with an empty namespace", inLogo(`<g xmlns:x=""/>`),
			[]string{"empty namespace for prefix x on <g>"}},
		{"a malformed element name", inLogo(`<path:/>`), []string{"malformed name <path:>"}},
		{"a malformed attribute name", inLogo(`<path :d="M0 0h10v10z"/>`), []string{"malformed name :d on <path>"}},
		{"a prefixed element name whose local part is no name", inLogo(`<a:1b xmlns:a="urn:a"/>`),
			[]string{"malformed name <a:1b>", "starts with a letter or an underscore"}},
		{"a prefixed attribute name whose local part is no name", inLogo(`<path xmlns:a="urn:a" a:.d="x"/>`),
			[]string{"malformed name a:.d on <path>"}},
		{"a character reference to a surrogate", inLogo(`<title>&#xD800;</title>`), []string{"&#xD800;"}},
		{"attributes with no white space between them", inLogo(`<path d="M0 0"fill="red"/>`),
			[]string{"white space before the attribute fill on <path>"}},
		{"nesting past the limit", nested(257), []string{"nesting 257 levels deep", "at most 256"}},
		{"an element name past the limit", inLogo("<" + nameOf("", 257) + "/>"),
			[]string{"name longer than 256 bytes", "the element", "257 bytes"}},
		{"an attribute name past the limit", inLogo("<path " + nameOf("", 257) + `="1"/>`),
			[]string{"name longer than 256 bytes", "the attribute", "of <path>", "257 bytes"}},
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

// TestReadStartTag: a start tag is read as it is written: its name, and each attribute by its name, its value as
// written and whether white space stands before it, with a quote closing the value only when it is the quote that
// opened it. The names are those the decoder reads from the same text without translating them.
func TestReadStartTag(t *testing.T) {
	e := string(rune(0xe9))
	cases := []struct {
		name, tag string
		want      startTag
	}{
		{"no attributes", `<svg>`, startTag{name: "svg"}},
		{"an empty tag", `<svg/>`, startTag{name: "svg"}},
		{"an empty tag after a space", `<svg />`, startTag{name: "svg"}},
		{"an attribute", `<a b="1">`, startTag{"a", []tagAttr{{"b", "1", true}}}},
		{"an attribute in single quotes", `<a b='1'/>`, startTag{"a", []tagAttr{{"b", "1", true}}}},
		{"an empty value", `<a b=""/>`, startTag{"a", []tagAttr{{"b", "", true}}}},
		{"attributes with no white space between them", `<a b="1"c='2'/>`,
			startTag{"a", []tagAttr{{"b", "1", true}, {"c", "2", false}}}},
		{"every kind of white space, around the equals sign too", "<a\tb\n=\r\"1\"\nc = '2'\r\n/>",
			startTag{"a", []tagAttr{{"b", "1", true}, {"c", "2", true}}}},
		{"values that hold the other quote, an equals sign and the end of a tag", `<a b='x"y=z>w' c="/>'">`,
			startTag{"a", []tagAttr{{"b", `x"y=z>w`, true}, {"c", "/>'", true}}}},
		{"prefixed names", `<x:a xmlns:x="u" x:b="1">`,
			startTag{"x:a", []tagAttr{{"xmlns:x", "u", true}, {"x:b", "1", true}}}},
		{"names with a colon at either end", `<x: :a="1" b:="2"/>`,
			startTag{"x:", []tagAttr{{":a", "1", true}, {"b:", "2", true}}}},
		{"letters beyond ASCII", "<" + e + " " + e + `="1"/>`, startTag{e, []tagAttr{{e, "1", true}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := readStartTag(c.tag)
			if got.name != c.want.name || !slices.Equal(got.attrs, c.want.attrs) {
				t.Errorf("read %+v, want %+v", got, c.want)
			}
			sameAsDecoder(t, c.tag, got)
		})
	}
}

// sameAsDecoder fails the test when the names and values of tag, which holds no reference, are not those the decoder
// reads from text without translating them.
func sameAsDecoder(t *testing.T, text string, tag startTag) {
	t.Helper()
	tok, err := xml.NewDecoder(strings.NewReader(text)).RawToken()
	start, ok := tok.(xml.StartElement)
	if err != nil || !ok {
		t.Fatalf("the decoder reads %q as %T, %v; want a start element", text, tok, err)
	}
	prefix, local := splitName(tag.name)
	if start.Name != (xml.Name{Space: prefix, Local: local}) || len(start.Attr) != len(tag.attrs) {
		t.Fatalf("the decoder reads the element %+v with %d attributes, the reader %q with %d",
			start.Name, len(start.Attr), tag.name, len(tag.attrs))
	}
	for i, a := range start.Attr {
		prefix, local := splitName(tag.attrs[i].name)
		if a.Name != (xml.Name{Space: prefix, Local: local}) || a.Value != tag.attrs[i].value {
			t.Errorf("the decoder reads the attribute %+v = %q, the reader %+v", a.Name, a.Value, tag.attrs[i])
		}
	}
}

// TestStartsName: the first character of the part after a colon is one that XML lets a name start with (production 4
// of XML 1.0, fifth edition, without the colon). Both sides of the end of every range are pinned.
func TestStartsName(t *testing.T) {
	starts := []rune{'A', 'Z', '_', 'a', 'z', 0xc0, 0xd6, 0xd8, 0xf6, 0xf8, 0x2ff, 0x370, 0x37d, 0x37f, 0x1fff, 0x200c,
		0x200d, 0x2070, 0x218f, 0x2c00, 0x2fef, 0x3001, 0xd7ff, 0xf900, 0xfdcf, 0xfdf0, 0xfffd, 0x10000, 0xeffff}
	others := []rune{'0', '9', '.', '-', ':', ' ', '@', '[', '`', '{', 0xb7, 0xbf, 0xd7, 0xf7, 0x300, 0x36f, 0x37e,
		0x2000, 0x200b, 0x200e, 0x203f, 0x206f, 0x2190, 0x2bff, 0x2ff0, 0x3000, 0xe000, 0xf8ff, 0xfdd0, 0xfdef, 0xfffe,
		0xf0000}
	for _, r := range starts {
		if !startsName(string(r) + "x") {
			t.Errorf("%U does not start a name, want it to", r)
		}
	}
	for _, r := range others {
		if startsName(string(r) + "x") {
			t.Errorf("%U starts a name, want it not to", r)
		}
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
