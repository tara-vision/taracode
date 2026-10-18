package docscheck

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The logo as the file writes it. The decoder hands out names by namespace and lets some things pass that XML does
// not, so the rules that need the text itself read each start tag, and each character reference, from the raw text of
// the token that readLogo walks.

// xmlSpace is the white space of XML: a space, a tab, a carriage return and a line feed.
const xmlSpace = " \t\r\n"

// surrogateFirst and surrogateLast are the ends of the surrogates, the code points XML has no character for.
const (
	surrogateFirst = 0xd800
	surrogateLast  = 0xdfff
)

// startTag is a start tag as written in the file: the name it is written with, prefix and colon included, and its
// attributes in order.
type startTag struct {
	name  string
	attrs []tagAttr
}

// tagAttr is an attribute as written: the name it is written with, its value between the quotes with the references
// still in it, and whether white space stands before it, after the element name or after the value before it.
type tagAttr struct {
	name, value string
	spaced      bool
}

// readStartTag reads the start tag raw, which the decoder has already read as well-formed: a <, a name, any number of
// attributes (white space, a name, an equals sign with white space around it, a value in quotes), and the end of the
// tag. A name ends at the first white space, equals sign, slash or >; a value ends at the quote that opened it, and
// the decoder lets a value hold neither a < nor that quote.
func readStartTag(raw string) startTag {
	var tag startTag
	rest := strings.TrimPrefix(raw, "<")
	tag.name, rest = cutName(rest)
	for {
		unspaced := rest
		rest = strings.TrimLeft(rest, xmlSpace)
		if rest == "" || rest[0] == '/' || rest[0] == '>' {
			return tag
		}
		attr := tagAttr{spaced: len(rest) < len(unspaced)}
		if attr.name, rest = cutName(rest); attr.name == "" {
			return tag
		}
		rest = strings.TrimLeft(rest, xmlSpace)
		rest = strings.TrimLeft(strings.TrimPrefix(rest, "="), xmlSpace)
		attr.value, rest = cutValue(rest)
		tag.attrs = append(tag.attrs, attr)
	}
}

// cutName splits the name off the front of s: everything up to the first white space, equals sign, slash or >.
func cutName(s string) (name, rest string) {
	end := strings.IndexAny(s, xmlSpace+"=/>")
	if end < 0 {
		return s, ""
	}
	return s[:end], s[end:]
}

// cutValue splits a value off the front of s, which opens with the quote that closes it: the value is what stands
// between the two.
func cutValue(s string) (value, rest string) {
	if s == "" {
		return "", ""
	}
	value, rest, _ = strings.Cut(s[1:], s[:1])
	return value, rest
}

// splitName splits a name as written into its prefix and its local part the way the decoder does: at the colon, unless
// the colon opens or ends the name, which leaves one name without a prefix. The decoder refuses a second colon itself.
func splitName(name string) (prefix, local string) {
	if p, l, ok := strings.Cut(name, ":"); ok && p != "" && l != "" {
		return p, l
	}
	return "", name
}

// nameStart holds the characters XML lets a name start with, but for the colon: production 4 of XML 1.0, fifth
// edition, which libxml and the browsers follow. The decoder checks the first character of the whole name only, so
// the part after a colon is judged here.
var nameStart = &unicode.RangeTable{
	R16: []unicode.Range16{
		{Lo: 'A', Hi: 'Z', Stride: 1}, {Lo: '_', Hi: '_', Stride: 1}, {Lo: 'a', Hi: 'z', Stride: 1},
		{Lo: 0xc0, Hi: 0xd6, Stride: 1}, {Lo: 0xd8, Hi: 0xf6, Stride: 1}, {Lo: 0xf8, Hi: 0x2ff, Stride: 1},
		{Lo: 0x370, Hi: 0x37d, Stride: 1}, {Lo: 0x37f, Hi: 0x1fff, Stride: 1}, {Lo: 0x200c, Hi: 0x200d, Stride: 1},
		{Lo: 0x2070, Hi: 0x218f, Stride: 1}, {Lo: 0x2c00, Hi: 0x2fef, Stride: 1}, {Lo: 0x3001, Hi: 0xd7ff, Stride: 1},
		{Lo: 0xf900, Hi: 0xfdcf, Stride: 1}, {Lo: 0xfdf0, Hi: 0xfffd, Stride: 1},
	},
	R32: []unicode.Range32{{Lo: 0x10000, Hi: 0xeffff, Stride: 1}},
}

// startsName reports whether s opens with a character that XML lets a name start with. An empty s, and bytes that are
// no UTF-8, open with none.
func startsName(s string) bool {
	if r, size := utf8.DecodeRuneInString(s); r != utf8.RuneError || size > 1 {
		return unicode.Is(nameStart, r)
	}
	return false
}

// charRef is a character reference as written: &# with decimal digits, or &#x with hexadecimal digits, and a semicolon.
// Only the lowercase x opens a hexadecimal one, as in XML and in the decoder.
var charRef = regexp.MustCompile(`&#(?:x([0-9A-Fa-f]+)|([0-9]+));`)

// surrogateRefIn is the first character reference in raw that names a surrogate, as written, or "" when raw holds
// none. The decoder reads such a reference as U+FFFD, which XML allows, so only the text as written shows it; a strict
// reader refuses it, as a surrogate is no XML character. Leading zeros do not change the value, and a literal U+FFFD
// is no reference.
func surrogateRefIn(raw string) string {
	if !strings.Contains(raw, "&#") {
		return ""
	}
	for _, m := range charRef.FindAllStringSubmatch(raw, -1) {
		digits, base := m[2], 10
		if m[1] != "" {
			digits, base = m[1], 16
		}
		if n, err := strconv.ParseUint(digits, base, 32); err == nil && n >= surrogateFirst && n <= surrogateLast {
			return m[0]
		}
	}
	return ""
}
