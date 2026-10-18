package docscheck

import (
	"encoding/xml"
	"errors"
	"strings"
)

// The logo rules about well-formed XML, as a strict reader such as the site's and a browser's reads it. checkLogo, in
// logos_rules_test.go, collects them with the rules about the SVG itself.

// checkLogoXML: the file is XML with exactly one root element and only characters XML allows. The decoder lets a
// second root pass without a word, and a character it does not read in a comment, so both are judged here. Only UTF-8
// is read: a file that declares another encoding, or holds a byte that is not UTF-8, is refused here with a word on
// how to mend it.
func checkLogoXML(label string, parts logoParts) []string {
	var p findings
	switch {
	case parts.badChar != "":
		p.addf("%s: not XML: %s", label, parts.badChar)
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

// checkLogoPrefixes: every prefix below the root is declared by an xmlns:<prefix> on its element or above it, judged as
// the prefix is written and not by the namespace it stands for, and no prefix is declared with an empty namespace. A
// browser that reads namespaces refuses a file with an unbound prefix, so the logo would not show; xmlns:xlink taken
// out while an xlink:href stays is the usual case. The root's own prefix is left to checkLogoNamespace.
func checkLogoPrefixes(label string, parts logoParts) []string {
	var p findings
	for _, u := range parts.unbound {
		switch {
		case u.empty:
			p.addf("%s: empty namespace for prefix %s on <%s> (xmlns:%s needs a namespace URI)",
				label, u.prefix, u.element, u.prefix)
		case u.attr == "":
			p.addf("%s: unbound prefix %s on <%s> (declare xmlns:%s on it or above it)", label, u.prefix, u.element, u.prefix)
		default:
			p.addf("%s: unbound prefix %s on the attribute %s of <%s> (declare xmlns:%s on it or above it)",
				label, u.prefix, u.attr, u.element, u.prefix)
		}
	}
	return p
}

// checkLogoNames: a name of an element or an attribute is a local name, or a prefix and a local name with one colon
// between them. The decoder refuses a second colon but takes a name with a colon at either end (<:x>, <x:>, :d="") as
// one local name, which a browser that reads namespaces refuses.
func checkLogoNames(label string, parts logoParts) []string {
	var p findings
	for _, e := range parts.elements {
		if strings.Contains(e.Name.Local, ":") {
			p.addf("%s: malformed name <%s> (a colon stands only between a prefix and a local name)", label, e.Name.Local)
		}
		for _, a := range e.Attr {
			if strings.Contains(a.Name.Local, ":") {
				p.addf("%s: malformed name %s on <%s> (a colon stands only between a prefix and a local name)",
					label, a.Name.Local, e.Name.Local)
			}
		}
	}
	return p
}

// checkLogoSurrogates: no character reference names a surrogate, U+D800 to U+DFFF, for which XML has no character. The
// decoder reads one as U+FFFD, which it allows, so the reference is found in the text as written. A strict parser
// refuses it in text and in attribute values; here it is refused in comments and CDATA sections too.
func checkLogoSurrogates(label string, parts logoParts) []string {
	var p findings
	if parts.surrogateRef != "" {
		p.addf("%s: character reference %.40s names a surrogate (U+D800 to U+DFFF are no characters of XML)",
			label, parts.surrogateRef)
	}
	return p
}

// checkLogoAttrSpacing: XML white space stands before every attribute. The decoder lets an attribute follow the closing
// quote of the one before it with nothing between them (a="1"b="2"), which a strict parser refuses.
func checkLogoAttrSpacing(label string, parts logoParts) []string {
	var p findings
	for _, tag := range parts.tags {
		for _, a := range tag.attrs {
			if !a.spaced {
				p.addf("%s: no white space before the attribute %s on <%s> (separate attributes with a space, a tab or a "+
					"line break)", label, a.name, tag.name)
			}
		}
	}
	return p
}

// checkLogoLocalNames: in a prefixed name, the part after the colon starts as a name does, with a letter, an underscore
// or another character that XML lets a name start with. The decoder checks the first character of the whole name only,
// so a:1b, a:.d and xmlns:6link pass it while a strict parser refuses them.
func checkLogoLocalNames(label string, parts logoParts) []string {
	var p findings
	for _, tag := range parts.tags {
		if badLocalPart(tag.name) {
			p.addf("%s: malformed name <%s> (the part after the colon starts with a letter or an underscore)",
				label, tag.name)
		}
		for _, a := range tag.attrs {
			if badLocalPart(a.name) {
				p.addf("%s: malformed name %s on <%s> (the part after the colon starts with a letter or an underscore)",
					label, a.name, tag.name)
			}
		}
	}
	return p
}

// badLocalPart reports whether name, as written, has a prefix and a local part that does not start a name.
func badLocalPart(name string) bool {
	prefix, local := splitName(name)
	return prefix != "" && !startsName(local)
}

// checkLogoNameLengths: no element or attribute name is longer than logoMaxName bytes as written, a prefix and its
// colon counted. The decoder reads a name of any length, and libxml, which the site parses with, refuses one past
// 50,000 bytes; a logo has no use for a name anywhere near that long.
func checkLogoNameLengths(label string, parts logoParts) []string {
	var p findings
	for _, tag := range parts.tags {
		if len(tag.name) > logoMaxName {
			p.addf("%s: name longer than %d bytes: the element %.40q is %d bytes", label, logoMaxName, tag.name, len(tag.name))
		}
		for _, a := range tag.attrs {
			if len(a.name) > logoMaxName {
				p.addf("%s: name longer than %d bytes: the attribute %.40q of <%.40s> is %d bytes",
					label, logoMaxName, a.name, tag.name, len(a.name))
			}
		}
	}
	return p
}

// checkLogoDepth: no element stands deeper than logoMaxDepth levels, the root svg being level 1. The decoder has no
// limit, libxml refuses an element at level 258 and a browser caps nesting too; a logo is one small shape.
func checkLogoDepth(label string, parts logoParts) []string {
	var p findings
	if parts.depth > logoMaxDepth {
		p.addf("%s: nesting %d levels deep (an element may stand at most %d levels down, the root svg being level 1)",
			label, parts.depth, logoMaxDepth)
	}
	return p
}
