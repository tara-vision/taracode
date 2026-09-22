// Package shellwords splits a POSIX shell command line into pipeline segments and words without
// running it: single and double quotes, backslash escapes, the control operators | || && ; & and
// newlines, redirections, comments, and a flag for command substitution ($(...) and backticks) so
// a classifier can refuse what it cannot see through.
package shellwords

import (
	"errors"
	"strings"
	"unicode"
)

// Segment is one simple command of a pipeline or list.
type Segment struct {
	Words      []string // the command and its arguments, quotes removed
	Redirects  []string // redirections in this segment, operator and target: "> out.log", "2>&1"
	Background bool     // the segment ends with &
}

// Result is a parsed command line.
type Result struct {
	Segments     []Segment
	Substitution bool // $(...) or a backtick appeared anywhere
}

type parser struct {
	in      []rune
	pos     int
	res     Result
	seg     Segment
	word    strings.Builder
	hasWord bool
	pending string // a redirection operator waiting for its target word
}

// Split parses a command line. It returns an error on an unbalanced quote.
func Split(command string) (Result, error) {
	p := &parser{in: []rune(command)}
	if err := p.run(); err != nil {
		return Result{}, err
	}
	return p.res, nil
}

// Words splits an argument string into words with the same quoting rules, ignoring operators.
func Words(args string) ([]string, error) {
	res, err := Split(args)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, s := range res.Segments {
		out = append(out, s.Words...)
	}
	return out, nil
}

func (p *parser) run() error {
	for p.pos < len(p.in) {
		if err := p.step(); err != nil {
			return err
		}
	}
	p.endWord()
	p.endSegment(false)
	return nil
}

// step consumes and handles the rune at the current position, advancing pos.
func (p *parser) step() error {
	c := p.in[p.pos]
	switch {
	case c == '\'':
		return p.quoted('\'')
	case c == '"':
		return p.quoted('"')
	case c == '\\' && p.pos+1 < len(p.in):
		p.word.WriteRune(p.in[p.pos+1])
		p.hasWord = true
		p.pos += 2
	case c == '#' && !p.hasWord:
		p.skipComment()
	case p.substitutionStart(c):
		p.res.Substitution = true
		p.word.WriteRune(c)
		p.pos++
	case c == '|' || c == '&' || c == ';' || c == '\n':
		p.operator(c)
	case c == '>' || c == '<':
		p.redirect()
	case unicode.IsSpace(c):
		p.endWord()
		p.pos++
	default:
		p.word.WriteRune(c)
		p.hasWord = true
		p.pos++
	}
	return nil
}

// substitutionStart reports whether c opens a backtick or $(...) command substitution.
func (p *parser) substitutionStart(c rune) bool {
	return c == '`' || (c == '$' && p.pos+1 < len(p.in) && p.in[p.pos+1] == '(')
}

func (p *parser) quoted(q rune) error {
	p.pos++ // opening quote
	for p.pos < len(p.in) {
		c := p.in[p.pos]
		switch {
		case c == q:
			p.hasWord = true
			p.pos++
			return nil
		case q == '"' && c == '\\' && p.pos+1 < len(p.in):
			p.word.WriteRune(p.in[p.pos+1])
			p.pos += 2
		case q == '"' && (c == '`' || (c == '$' && p.pos+1 < len(p.in) && p.in[p.pos+1] == '(')):
			p.res.Substitution = true
			p.word.WriteRune(c)
			p.pos++
		default:
			p.word.WriteRune(c)
			p.pos++
		}
	}
	return errors.New("unbalanced quote")
}

func (p *parser) skipComment() {
	for p.pos < len(p.in) && p.in[p.pos] != '\n' {
		p.pos++
	}
}

func (p *parser) operator(c rune) {
	p.endWord()
	next := rune(0)
	if p.pos+1 < len(p.in) {
		next = p.in[p.pos+1]
	}
	switch {
	case c == '|' && next == '|', c == '&' && next == '&':
		p.pos += 2
		p.endSegment(false)
	case c == '&' && next == '>':
		p.redirect() // &> file
	case c == '&':
		p.pos++
		p.endSegment(true)
	default:
		p.pos++
		p.endSegment(false)
	}
}

// redirect consumes an operator such as >, >>, <, 2>, >&, 2>&1, &> and remembers it; a target word
// follows unless the operator already named a descriptor (2>&1).
func (p *parser) redirect() {
	fd := ""
	if p.hasWord && isDigits(p.word.String()) {
		fd = p.word.String()
		p.word.Reset()
		p.hasWord = false
	}
	start := p.pos
	for p.pos < len(p.in) && strings.ContainsRune("<>&", p.in[p.pos]) {
		p.pos++
	}
	op := fd + string(p.in[start:p.pos])
	if strings.HasSuffix(op, "&") { // descriptor duplication: the target is a number or -
		tstart := p.pos
		for p.pos < len(p.in) && (unicode.IsDigit(p.in[p.pos]) || p.in[p.pos] == '-') {
			p.pos++
		}
		p.seg.Redirects = append(p.seg.Redirects, op+string(p.in[tstart:p.pos]))
		return
	}
	p.pending = op
}

func (p *parser) endWord() {
	if !p.hasWord {
		return
	}
	w := p.word.String()
	p.word.Reset()
	p.hasWord = false
	if p.pending != "" {
		p.seg.Redirects = append(p.seg.Redirects, p.pending+" "+w)
		p.pending = ""
		return
	}
	p.seg.Words = append(p.seg.Words, w)
}

func (p *parser) endSegment(background bool) {
	if len(p.seg.Words) == 0 && len(p.seg.Redirects) == 0 {
		return
	}
	p.seg.Background = background
	p.res.Segments = append(p.res.Segments, p.seg)
	p.seg = Segment{}
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
