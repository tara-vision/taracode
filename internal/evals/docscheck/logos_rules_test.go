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
	"unicode"
	"unicode/utf8"
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

// xmlPrefix is the prefix of the xml namespace, which is bound without a declaration, and xmlnsPrefix the prefix of a
// declaration: xmlns:<prefix> declares a prefix.
const (
	xmlPrefix   = "xml"
	xmlnsPrefix = "xmlns"
)

// xmlNamespace and xmlnsNamespace are the namespaces of those two prefixes. Each belongs to its own prefix alone: no
// other prefix is bound to either.
const (
	xmlNamespace   = "http://www.w3.org/XML/1998/namespace"
	xmlnsNamespace = "http://www.w3.org/2000/xmlns/"
)

// The most a logo asks of a strict reader: elements nest at most logoMaxDepth levels deep, the root svg being level 1,
// and a name is at most logoMaxName bytes long as written, a prefix and its colon counted. Both sit far inside the
// limits of libxml, which the site parses with (257 levels, 50,000 bytes for a name), and a browser caps nesting too.
const (
	logoMaxDepth = 256
	logoMaxName  = 256
)

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
	tags         []startTag         // the start tag of every element as written, in the order of elements
	unbound      []unboundPrefix    // every prefix that no xmlns:<prefix> in scope declares, or that is declared empty
	depth        int                // the deepest level an element stands at; the root is level 1
	surrogateRef string             // the first character reference to a surrogate, as written; "" when none
	badChar      string             // where the file holds a character XML does not allow; "" when it holds none
	err          error              // why the walk stopped before the end of the file: it is not well-formed XML
}

// unboundPrefix is a prefix, as written, that no xmlns:<prefix> on its element or above declares, with the element it
// stands on and, for an attribute, the attribute's local name; or, when empty is set, a prefix that the element
// declares with an empty namespace.
type unboundPrefix struct {
	prefix, element, attr string
	empty                 bool
}

// isXMLChar reports whether r is a character of XML 1.0: a tab, a line feed, a carriage return, or a character from
// U+0020 on that is neither a surrogate nor U+FFFE or U+FFFF.
func isXMLChar(r rune) bool {
	return r == '\t' || r == '\n' || r == '\r' || r >= 0x20 && r <= 0xd7ff || r >= 0xe000 && r <= 0xfffd ||
		r >= 0x10000 && r <= unicode.MaxRune
}

// badCharIn says where data holds the first character that XML does not allow, or "" when it holds none: a byte that
// is not UTF-8, a control character other than a tab, a line feed or a carriage return, or U+FFFE or U+FFFF. The
// decoder checks text and attribute values itself but not comments, which a strict parser refuses all the same.
func badCharIn(data []byte) string {
	for i := 0; i < len(data); {
		r, size := utf8.DecodeRune(data[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			return fmt.Sprintf("a byte that is not UTF-8 at byte %d (save the file as UTF-8)", i)
		case !isXMLChar(r):
			return fmt.Sprintf("character %U at byte %d, which XML does not allow", r, i)
		}
		i += size
	}
	return ""
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
	scopes     [][]string      // the prefixes that each open element declares, the innermost last
}

// readLogo reads a logo: it finds the first character that XML does not allow, reads past a byte order mark that opens
// the file, and walks the XML tokens once. The walk stops at the first syntax error and keeps what it read before it,
// and the rules judge only what they find. A CDATA section arrives as text, so the style rules read it like any
// other text, and the text nodes of a style element are joined with the comments left out, the way a browser reads its
// CSS. Each token is also taken as written in the file, so that a declaration is known to open the file or not, so
// that text outside the root is judged by its bytes (a CDATA section or a character reference there is text, even when
// it stands for a space), and so that a start tag is judged by its names, its spacing and its references as written,
// which the decoder has translated, let pass or replaced.
func readLogo(file []byte) logoParts {
	var w logoWalk
	w.parts.badChar = badCharIn(file)
	data := bytes.TrimPrefix(file, []byte(utf8BOM))
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

// take files one token. raw is the token as written, and first says whether it opens the file. A character reference to
// a surrogate is looked for in the text as written, in every kind of token, where the decoder has already replaced it.
func (w *logoWalk) take(tok xml.Token, raw string, first bool) {
	w.parts.surrogateRef = cmp.Or(w.parts.surrogateRef, surrogateRefIn(raw))
	switch t := tok.(type) {
	case xml.StartElement:
		w.open(t, readStartTag(raw))
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

// open files an element with its start tag as written, counts it as a root when it stands at the top, notes how deep
// the walk gets and a style element it enters, and takes the prefixes the element declares into scope before it looks
// for unbound ones.
func (w *logoWalk) open(t xml.StartElement, tag startTag) {
	w.depth++
	w.parts.depth = max(w.parts.depth, w.depth)
	w.parts.elements = append(w.parts.elements, t)
	w.parts.tags = append(w.parts.tags, tag)
	w.scopes = append(w.scopes, declaredPrefixes(t))
	w.parts.unbound = append(w.parts.unbound, w.unboundIn(tag)...)
	if w.depth == 1 {
		w.parts.roots++
	}
	if w.styleDepth == 0 && lowerLocal(t.Name) == "style" {
		w.styleDepth = w.depth
	}
}

// close ends an element, takes its prefixes out of scope, and with the outermost style element files the text of that
// style.
func (w *logoWalk) close() {
	if w.depth == w.styleDepth {
		w.parts.styles = append(w.parts.styles, w.css.String())
		w.css.Reset()
		w.styleDepth = 0
	}
	w.scopes = w.scopes[:len(w.scopes)-1]
	w.depth--
}

// declaredPrefixes lists the prefixes an element declares with xmlns:<prefix>, as written. A declaration binds its
// prefix unless libxml drops it, which the value decides, as the decoder has read it: the xml namespace and the xmlns
// namespace belong to the prefixes xml and xmlns alone, so a prefix declared for either stays unbound, and the prefix
// xmlns cannot be declared at all. A declaration with an empty namespace is listed, as it is refused by a rule of its
// own.
func declaredPrefixes(t xml.StartElement) []string {
	var out []string
	for _, a := range t.Attr {
		if a.Name.Space == xmlnsPrefix && a.Name.Local != xmlnsPrefix && a.Value != xmlNamespace &&
			a.Value != xmlnsNamespace {
			out = append(out, a.Name.Local)
		}
	}
	return out
}

// unboundIn lists the prefixes of an element and of its attributes that no xmlns:<prefix> declares on the element or
// above it. A prefix is judged as written: the decoder puts the namespace of a bound prefix in a name and leaves an
// unbound prefix as it is, so a name's namespace cannot tell a bound prefix from an unbound one that is spelled like a
// namespace in scope. The root's own name is left to checkLogoNamespace. A declaration is no prefix, but one that
// declares a prefix with an empty namespace (xmlns:x="", which XML forbids while it allows xmlns="") is listed too.
func (w *logoWalk) unboundIn(tag startTag) []unboundPrefix {
	var out []unboundPrefix
	prefix, element := splitName(tag.name)
	if w.depth > 1 && !w.bound(prefix) {
		out = append(out, unboundPrefix{prefix: prefix, element: element})
	}
	for _, a := range tag.attrs {
		prefix, local := splitName(a.name)
		switch {
		case prefix == xmlnsPrefix && a.value == "":
			out = append(out, unboundPrefix{prefix: local, element: element, empty: true})
		case prefix != xmlnsPrefix && !w.bound(prefix):
			out = append(out, unboundPrefix{prefix: prefix, element: element, attr: local})
		}
	}
	return out
}

// bound reports whether prefix needs no declaration (no prefix, or xml) or is declared by an open element.
func (w *logoWalk) bound(prefix string) bool {
	return prefix == "" || prefix == xmlPrefix ||
		slices.ContainsFunc(w.scopes, func(declared []string) bool { return slices.Contains(declared, prefix) })
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
// refuses a logo that passed here (only characters XML allows, comments included, and no character reference to a
// surrogate; a byte order mark that opens the file is read past; before the root stand only blank lines, comments and
// an XML declaration that opens the file, after it only blank lines and comments; no other processing instruction;
// white space before every attribute and no attribute twice; no unbound prefix, judged as written; no malformed name,
// and a part after the colon that starts like a name; no name over 256 bytes and no element deeper than 256 levels);
// one root svg in the SVG namespace with a viewBox; no DOCTYPE; no script, foreignObject, image or iframe element; no
// style element that holds @import, url( or a backslash; no on* attribute and no href that is not a #fragment. Every
// rule is a function of its own.
func checkLogo(name string, data []byte) []string {
	label, parts := fmt.Sprintf("logo %q", name), readLogo(data)
	return slices.Concat(
		checkLogoSize(label, data),
		checkLogoXML(label, parts),
		checkLogoSurrogates(label, parts),
		checkLogoTextBefore(label, parts),
		checkLogoTextAfter(label, parts),
		checkLogoDeclarationPlace(label, parts),
		checkLogoDeclarationForm(label, parts),
		checkLogoInstructions(label, parts),
		checkLogoAttrSpacing(label, parts),
		checkLogoDuplicateAttrs(label, parts),
		checkLogoPrefixes(label, parts),
		checkLogoNames(label, parts),
		checkLogoLocalNames(label, parts),
		checkLogoNameLengths(label, parts),
		checkLogoDepth(label, parts),
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
