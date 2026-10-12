package shellwords

import (
	"errors"
	"strings"
)

// dollarExpansion reads $((...)), $[...] and ${...} at the current position as part of the word: a << in them is
// a shift or a pattern, not a here-document. handled is false for a $( that opens a command substitution, which
// step captures.
func (p *parser) dollarExpansion() (handled bool, err error) {
	var end int
	switch p.next() {
	case '(':
		e, ok, closed := arithmeticEnd(p.in, p.pos+1)
		if !closed && p.pos+2 < len(p.in) && p.in[p.pos+2] == '(' {
			return true, errUnclosed
		}
		if !ok {
			return false, nil
		}
		end = e
	case '[':
		end = closingBracket(p.in, p.pos+1)
	default:
		end = parameterEnd(p.in, p.pos)
	}
	if end >= len(p.in) {
		return true, errUnclosed
	}
	p.expansionWord(p.pos, end)
	return true, nil
}

// expansionWord takes the expansion from start to end, inclusive, into the current word, capturing the command
// substitutions inside it; nothing in it is an operator or a redirect.
func (p *parser) expansionWord(start, end int) {
	p.scanExpansions(p.in[start+2:end], start >= p.outerEnd)
	p.word.WriteString(string(p.in[start : end+1]))
	p.hasWord = true
	p.pos = end + 1
}

// arithmeticCommand reads the arithmetic command ((...)) that ends at end as one word, as bash runs it, so a <<
// in it hides no later line, and reads its inside as commands too, since sh may be dash, which runs ((...)) as
// nested subshells. Those commands come after the line's own segments, so its here-documents keep their places,
// and no here-document is read among them.
func (p *parser) arithmeticCommand(end int) error {
	inner := &parser{in: p.in[p.pos+1 : end], shell: true, noHeredocs: true}
	if err := inner.run(); err != nil {
		return err
	}
	p.res.Substitution = p.res.Substitution || inner.res.Substitution
	p.res.Backtick = p.res.Backtick || inner.res.Backtick
	p.res.FunctionDef = p.res.FunctionDef || inner.res.FunctionDef
	p.res.Substitutions = append(p.res.Substitutions, inner.res.Substitutions...)
	p.res.outer = append(p.res.outer, inner.res.outer...)
	p.extra = append(p.extra, inner.res.Segments...)
	p.word.WriteString(string(p.in[p.pos : end+1]))
	p.hasWord = true
	p.pos = end + 1
	return nil
}

// arithmeticEnd returns, for the (( at open, the index of the ) that closes it: ok is true when the inner
// parenthesis closes right before the outer one, as in $((...)) and ((...)), and false for anything else, such
// as a command substitution that starts with a subshell; closed is false when nothing closes the ((.
func arithmeticEnd(in []rune, open int) (end int, ok, closed bool) {
	if open+1 >= len(in) || in[open] != '(' || in[open+1] != '(' {
		return 0, false, true
	}
	depth, inner := 0, -1
	for i := open; i < len(in); i++ {
		if end, nested := nestedEnd(in, i); nested {
			i = end
			continue
		}
		switch in[i] {
		case '(':
			depth++
		case ')':
			if depth--; depth == 1 && inner < 0 {
				inner = i
			} else if depth == 0 {
				return i, inner == i-1, true
			}
		}
	}
	return len(in), false, false
}

// closingBracket returns the index of the ] that closes the [ at open, or the end when none does; a bracket
// in a quoted string or a nested expansion does not count (nestedEnd).
func closingBracket(in []rune, open int) int {
	depth := 0
	for i := open; i < len(in); i++ {
		if end, nested := nestedEnd(in, i); nested {
			i = end
			continue
		}
		switch in[i] {
		case '[':
			depth++
		case ']':
			if depth--; depth == 0 {
				return i
			}
		}
	}
	return len(in)
}

// parameterEnd returns the index of the } that closes the ${ whose $ is at dollar, as the shell finds it: a
// quote or a backslash keeps a } from closing it, and a ${, $( or backtick inside is a span of its own; the end
// of the input when nothing closes it.
func parameterEnd(in []rune, dollar int) int {
	for i := dollar + 2; i < len(in); i++ {
		if end, nested := nestedEnd(in, i); nested {
			i = end
		} else if in[i] == '}' {
			return i
		}
	}
	return len(in)
}

// nestedEnd returns the index of the last rune of a span that starts at i and keeps the brackets of an
// enclosing expansion from counting, as the shell reads it: an escaped character, a quoted string, a backtick
// substitution, a ${...} or a $(...); nested is false when none starts at i.
func nestedEnd(in []rune, i int) (end int, nested bool) {
	switch {
	case in[i] == '\\':
		return i + 1, true
	case in[i] == '\'':
		return skipQuoted(in, i, false), true
	case in[i] == '"':
		return skipQuoted(in, i, true), true
	case in[i] == '`':
		_, end = backtickBody(in, i)
		return end, true
	case in[i] == '$' && i+1 < len(in) && in[i+1] == '{':
		return parameterEnd(in, i), true
	case in[i] == '$' && i+1 < len(in) && in[i+1] == '(':
		return i + 2 + len([]rune((&parser{in: in, pos: i}).captureSubstitution())), true
	}
	return i, false
}

// dollarParen reads the $( at the current position inside double quotes: an arithmetic $((...)) as part of the
// word, any other as a command substitution, captured.
func (p *parser) dollarParen() {
	if end, ok, _ := arithmeticEnd(p.in, p.pos+1); ok {
		p.expansionWord(p.pos, end)
		return
	}
	p.res.Substitution = true
	body := p.captureSubstitution()
	p.addSubstitution(body, p.pos, p.pos+2+len([]rune(body)))
	p.word.WriteRune('$')
	p.pos++
}

// backtick flags the backtick at the current position and, when it opens a substitution rather than closing one,
// captures the command it runs into Substitutions, as a $(...) body is. The position does not move: the body is
// still read as words of the line.
func (p *parser) backtick() {
	p.res.Backtick = true
	if p.closingTick[p.pos] {
		return
	}
	body, end := backtickBody(p.in, p.pos)
	p.addSubstitution(body, p.pos, end)
	if p.closingTick == nil {
		p.closingTick = map[int]bool{}
	}
	p.closingTick[end] = true
}

// backtickBody returns the command of the backtick substitution that opens at start, up to the backtick that closes
// it (the end of the input when none does), with the backslashes before a $, a backtick or a backslash removed as
// the shell removes them, and the index of that closing backtick.
func backtickBody(in []rune, start int) (string, int) {
	var b strings.Builder
	for i := start + 1; i < len(in); i++ {
		switch {
		case in[i] == '\\' && i+1 < len(in) && strings.ContainsRune("$`\\", in[i+1]):
			i++
			b.WriteRune(in[i])
		case in[i] == '`':
			return b.String(), i
		default:
			b.WriteRune(in[i])
		}
	}
	return b.String(), len(in)
}

// scanExpansions captures the command substitutions of text the shell expands apart from the parse of the line
// (an expanded here-document body, the inside of an arithmetic or parameter expansion) as a double-quoted
// string's are captured: a backslash escapes a $ or a backtick, a $((...)) is arithmetic, and a substitution
// inside another is reached through it. outer tells whether the text lies outside every captured substitution.
func (p *parser) scanExpansions(in []rune, outer bool) {
	for i := 0; i < len(in); i++ {
		switch {
		case in[i] == '\\':
			i++
		case in[i] == '`':
			body, end := backtickBody(in, i)
			p.res.Substitution, p.res.Backtick = true, true
			p.res.Substitutions, p.res.outer = append(p.res.Substitutions, body), append(p.res.outer, outer)
			i = end
		case in[i] == '$' && i+1 < len(in) && in[i+1] == '(':
			if _, arithmetic, _ := arithmeticEnd(in, i+1); !arithmetic {
				body := (&parser{in: in, pos: i}).captureSubstitution()
				p.res.Substitution = true
				p.res.Substitutions, p.res.outer = append(p.res.Substitutions, body), append(p.res.outer, outer)
				i += 2 + len([]rune(body)) // the ) that closes it
			}
		}
	}
}

// addSubstitution records a substitution body captured from start to end in the input: outer unless an earlier
// capture holds it.
func (p *parser) addSubstitution(body string, start, end int) {
	outer := start >= p.outerEnd
	if outer {
		p.outerEnd = end
	}
	p.res.Substitutions = append(p.res.Substitutions, body)
	p.res.outer = append(p.res.outer, outer)
}

// OuterSubstitutions are the bodies of the substitutions no other captured substitution holds: reading each of
// them as a line of its own reaches the ones nested in it, so each body is read once.
func (r Result) OuterSubstitutions() []string {
	var out []string
	for i, body := range r.Substitutions {
		if i < len(r.outer) && r.outer[i] {
			out = append(out, body)
		}
	}
	return out
}

// errUnclosed refuses an arithmetic or parameter expansion that nothing closes.
var errUnclosed = errors.New("an arithmetic or parameter expansion is not closed")
