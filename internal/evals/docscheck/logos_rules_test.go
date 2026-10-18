package docscheck

import (
	"bytes"
	"cmp"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
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

// svgNamespace is the namespace of SVG. A browser draws a root svg only in this namespace.
const svgNamespace = "http://www.w3.org/2000/svg"

// viewBoxName is the name of the viewBox attribute: spelled so, and in no namespace.
var viewBoxName = xml.Name{Local: "viewBox"}

// xmlDeclaration is the XML declaration a logo may open with: version 1.0, then optionally the encoding UTF-8 (in any
// letter case) and a standalone yes or no, in that order, each value in single or double quotes, where _ stands for
// the white space of XML. A strict XML parser refuses a declaration without a version or with anything else in it.
var xmlDeclaration = regexp.MustCompile(strings.ReplaceAll(
	`^<\?xml_+version_*=_*("1\.0"|'1\.0')(_+encoding_*=_*("(?i:utf-8)"|'(?i:utf-8)'))?`+
		`(_+standalone_*=_*("yes"|'yes'|"no"|'no'))?_*\?>$`, "_", `[ \t\r\n]`))

// errNotUTF8 is why a logo that declares another encoding is not read: the rules, like the page, read UTF-8 only.
var errNotUTF8 = errors.New("only UTF-8 is read")

// logoParts is what the rules read of a logo, collected in one walk over the tokens of its XML.
type logoParts struct {
	elements     []xml.StartElement // every element in document order; the first is the root of a well-formed file
	roots        int                // how many elements stand at the top of the file; a logo has one
	directives   int                // how many <!...> declarations the file holds; a DOCTYPE is one
	styles       []string           // the text of each style element, its text nodes joined
	declaration  string             // the XML declaration that opens the file, as written; "" when none does
	misplaced    int                // how many XML declarations stand anywhere but at the very start of the file
	instructions []string           // the target of every other processing instruction, in document order
	textBefore   string             // the first text before the root that is not blank, as written
	textAfter    string             // the first text after the root that is not blank, as written
	err          error              // why the walk stopped before the end of the file: it is not well-formed XML
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

// blank reports whether s holds nothing but the white space of XML: spaces, tabs, carriage returns and line feeds.
func blank(s string) bool { return strings.Trim(s, " \t\r\n") == "" }

// logoWalk is the state of the one walk over the tokens of a logo: what it has collected, how deep it is among the
// elements, and the style element it is in.
type logoWalk struct {
	parts      logoParts
	css        strings.Builder // the text of the style element being read
	depth      int             // how deep the walk is
	styleDepth int             // the depth of the outermost style element the walk is in; 0 when it is in none
}

// readLogo walks the XML tokens of a logo once. The walk stops at the first syntax error and keeps what it read before
// it, and the rules judge only what they find. A CDATA section arrives as text, so the style rules read it like any
// other text, and the text nodes of a style element are joined with the comments left out, the way a browser reads its
// CSS. Each token is also taken as written in the file, so that a declaration is known to open the file or not, and so
// that text outside the root is judged by its bytes: a CDATA section or a character reference there is text, even
// when it stands for a space.
func readLogo(data []byte) logoParts {
	var w logoWalk
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.CharsetReader = func(string, io.Reader) (io.Reader, error) { return nil, errNotUTF8 }
	for {
		start := dec.InputOffset()
		tok, err := dec.Token()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				w.parts.err = err
			}
			return w.parts
		}
		w.take(tok, string(data[start:dec.InputOffset()]), start == 0)
	}
}

// take files one token. raw is the token as written, and first says whether it opens the file.
func (w *logoWalk) take(tok xml.Token, raw string, first bool) {
	switch t := tok.(type) {
	case xml.StartElement:
		w.open(t)
	case xml.EndElement:
		w.close()
	case xml.CharData:
		w.text(t, raw)
	case xml.ProcInst:
		w.instruction(t, raw, first)
	case xml.Directive:
		w.parts.directives++
	}
}

// open files an element, counts it as a root when it stands at the top, and notes a style element the walk enters.
func (w *logoWalk) open(t xml.StartElement) {
	w.depth++
	w.parts.elements = append(w.parts.elements, t)
	if w.depth == 1 {
		w.parts.roots++
	}
	if w.styleDepth == 0 && lowerLocal(t.Name) == "style" {
		w.styleDepth = w.depth
	}
}

// close ends an element, and with the outermost style element files the text of that style.
func (w *logoWalk) close() {
	if w.depth == w.styleDepth {
		w.parts.styles = append(w.parts.styles, w.css.String())
		w.css.Reset()
		w.styleDepth = 0
	}
	w.depth--
}

// text files text: in a style element as its CSS, and outside the root, when it is not blank, as text before or after
// the root.
func (w *logoWalk) text(t xml.CharData, raw string) {
	switch {
	case w.styleDepth > 0:
		w.css.Write(t)
	case w.depth == 0 && !blank(raw):
		w.outside(raw)
	}
}

// outside keeps the first text that is not blank before the root, and the first after it.
func (w *logoWalk) outside(raw string) {
	if w.parts.roots == 0 {
		w.parts.textBefore = cmp.Or(w.parts.textBefore, raw)
		return
	}
	w.parts.textAfter = cmp.Or(w.parts.textAfter, raw)
}

// instruction files a processing instruction: the XML declaration when it opens the file, a declaration out of place
// anywhere else, and any other instruction by its target.
func (w *logoWalk) instruction(t xml.ProcInst, raw string, first bool) {
	switch {
	case t.Target != "xml":
		w.parts.instructions = append(w.parts.instructions, t.Target)
	case first:
		w.parts.declaration = raw
	default:
		w.parts.misplaced++
	}
}

// checkLogo applies the logo rules of the design to the bytes of the file called name and returns one message per
// problem: a file of at most 50 KB; well-formed XML in UTF-8 the way a strict parser reads it, so that the site never
// refuses a logo that passed here (a byte order mark that opens the file is read past; before the root stand only
// blank lines, comments and an XML declaration that opens the file, after it only blank lines and comments; no other
// processing instruction and no attribute twice); one root svg in the SVG namespace with a viewBox; no DOCTYPE; no
// script, foreignObject, image or iframe element; no style element that holds @import, url( or a backslash; no on*
// attribute and no href that is not a #fragment. Every rule is a function of its own.
func checkLogo(name string, data []byte) []string {
	label, parts := fmt.Sprintf("logo %q", name), readLogo(bytes.TrimPrefix(data, []byte(utf8BOM)))
	return slices.Concat(
		checkLogoSize(label, data),
		checkLogoXML(label, parts),
		checkLogoTextBefore(label, parts),
		checkLogoTextAfter(label, parts),
		checkLogoDeclarationPlace(label, parts),
		checkLogoDeclarationForm(label, parts),
		checkLogoInstructions(label, parts),
		checkLogoDuplicateAttrs(label, parts),
		checkLogoRoot(label, parts),
		checkLogoNamespace(label, parts),
		checkLogoViewBox(label, parts),
		checkLogoDoctype(label, parts),
		checkLogoElements(label, parts),
		checkLogoStyleImports(label, parts),
		checkLogoStyleURLs(label, parts),
		checkLogoStyleEscapes(label, parts),
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
// the root count is made here. Only UTF-8 is read: a file that declares another encoding is refused here, with a word
// on how to mend it.
func checkLogoXML(label string, parts logoParts) []string {
	var p findings
	switch {
	case errors.Is(parts.err, errNotUTF8):
		p.addf("%s: not XML: %v (save the file as UTF-8)", label, parts.err)
	case parts.err != nil:
		p.addf("%s: not XML: %v", label, parts.err)
	case parts.roots == 0:
		p.addf("%s: not XML: no root element", label)
	case parts.roots > 1:
		p.addf("%s: not XML: %d root elements (a logo has one svg)", label, parts.roots)
	}
	return p
}

// checkLogoTextBefore: nothing but blank lines, comments and the XML declaration stands before the root, as a strict
// XML parser wants: a CDATA section or a character reference there is text too. A file with no root is left to
// checkLogoXML.
func checkLogoTextBefore(label string, parts logoParts) []string {
	var p findings
	if parts.roots > 0 && parts.textBefore != "" {
		p.addf("%s: text before the root: %.40q (only blank lines, comments and the XML declaration come before <svg>)",
			label, parts.textBefore)
	}
	return p
}

// checkLogoTextAfter: nothing but blank lines and comments follows the root, as a strict XML parser wants.
func checkLogoTextAfter(label string, parts logoParts) []string {
	var p findings
	if parts.textAfter != "" {
		p.addf("%s: text after the root: %.40q (only blank lines and comments come after </svg>)", label, parts.textAfter)
	}
	return p
}

// checkLogoDeclarationPlace: an XML declaration opens the file or is not there. A strict XML parser refuses one after
// anything else, a blank line or a first declaration included.
func checkLogoDeclarationPlace(label string, parts logoParts) []string {
	var p findings
	if parts.misplaced > 0 {
		p.addf("%s: xml declaration out of place (it may only open the file, once, with nothing before it)", label)
	}
	return p
}

// checkLogoDeclarationForm: the declaration that opens the file says version 1.0, and after it at most the encoding
// UTF-8 and a standalone yes or no, as xmlDeclaration spells out.
func checkLogoDeclarationForm(label string, parts logoParts) []string {
	var p findings
	if parts.declaration != "" && !xmlDeclaration.MatchString(parts.declaration) {
		p.addf(`%s: malformed xml declaration %.80q (write <?xml version="1.0"?>, with encoding="UTF-8" and `+
			`standalone="yes" or "no" after the version if at all)`, label, parts.declaration)
	}
	return p
}

// checkLogoInstructions: no processing instruction but the XML declaration. An <?xml-stylesheet?> loads a stylesheet,
// and no other kind belongs in a logo.
func checkLogoInstructions(label string, parts logoParts) []string {
	var p findings
	for _, target := range parts.instructions {
		p.addf("%s: processing instruction <?%s?> (a logo holds none but the XML declaration that opens it)",
			label, target)
	}
	return p
}

// checkLogoDuplicateAttrs: no attribute twice on one element, which a strict XML parser refuses. Two attributes are one
// when their names and namespaces are: xlink:href and x:href, with x bound to XLink, are the same attribute twice.
func checkLogoDuplicateAttrs(label string, parts logoParts) []string {
	var p findings
	for _, e := range parts.elements {
		seen := map[xml.Name]bool{}
		for _, a := range e.Attr {
			if seen[a.Name] {
				p.addf("%s: duplicate attribute %s on <%s> (a strict XML parser refuses it)",
					label, a.Name.Local, e.Name.Local)
			}
			seen[a.Name] = true
		}
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

// checkLogoNamespace: the root svg is in the SVG namespace. Without xmlns, or with a prefix bound to nothing or to
// another namespace, it is an element a browser does not know, and its mask draws an empty box. A root that is not
// svg is left to checkLogoRoot.
func checkLogoNamespace(label string, parts logoParts) []string {
	var p findings
	if root, ok := parts.svgRoot(); ok && root.Name.Space != svgNamespace {
		p.addf(`%s: root svg not in the SVG namespace (it needs xmlns="%s", or its prefix bound to that namespace)`,
			label, svgNamespace)
	}
	return p
}

// checkLogoViewBox: the root svg has a viewBox with a value, which is what the page scales the logo by. The attribute
// is spelled viewBox and has no prefix, as XML names are case-sensitive and a viewbox or an x:viewBox is ignored. A
// root that is not svg is left to checkLogoRoot.
func checkLogoViewBox(label string, parts logoParts) []string {
	var p findings
	root, ok := parts.svgRoot()
	if !ok {
		return p
	}
	i := slices.IndexFunc(root.Attr, func(a xml.Attr) bool { return a.Name == viewBoxName })
	switch {
	case i < 0:
		p.addf("%s: no viewBox on the root svg (the page scales the logo by it)", label)
	case blank(root.Attr[i].Value):
		p.addf("%s: empty viewBox on the root svg (the page scales the logo by it)", label)
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

// checkLogoStyleEscapes: no style element holds a backslash. A CSS escape spells @import or url( in other characters
// (@\69mport, u\72l(), past the two rules above.
func checkLogoStyleEscapes(label string, parts logoParts) []string {
	var p findings
	if styleHolds(parts.styles, `\`) {
		p.addf("%s: style escape: a style element holds a backslash (a CSS escape can spell @import or url( in other "+
			"characters)", label)
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
