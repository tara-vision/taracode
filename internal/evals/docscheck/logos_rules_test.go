package docscheck

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
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
