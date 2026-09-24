// Package shellwords splits a POSIX shell command line into pipeline segments and words without
// running it: single and double quotes, bash's $'...' quoting (truncated at a NUL as bash does),
// backslash escapes, the control operators | || && ; & and newlines, the grouping operators ( and ),
// redirections, comments, a flag for what it cannot see through (command substitution: $(...),
// backticks and the process substitutions <(...) and >(...); and bash's translated $"..."), and a
// flag for a function definition (name (), function name) so a classifier can refuse it.
package shellwords

import (
	"errors"
	"strings"
	"unicode"
)

// Segment is one simple command of a pipeline or list.
type Segment struct {
	Words         []string // the command and its arguments, quotes removed
	Redirects     []string // redirections in this segment, operator and target: "> out.log", "2>&1"
	Background    bool     // the segment ends with &
	Parenthesized bool     // a ( or ) ended this segment: a subshell's body, or the one before a case arm
}

// Result is a parsed command line.
type Result struct {
	Segments     []Segment
	Substitution bool // $(...), a backtick, <(...), >(...) or $"..." appeared anywhere
	FunctionDef  bool // the line defines a shell function (name (), function name), which can shadow any name
}

type parser struct {
	in          []rune
	pos         int
	res         Result
	seg         Segment
	word        strings.Builder
	hasWord     bool
	pending     string // a redirection operator waiting for its target word
	shell       bool   // parse as sh -c does: ( and ) are operators, $'...' and $"..." are bash's quoting
	closed      bool   // the last segment was ended by a closing )
	ansiStopped bool   // a NUL was decoded inside the current $'...': the rest of the quote is discarded
	parenDepth  int    // ( seen minus ) seen so far; never negative, since a case arm's ) has no matching (
}

// Split parses a command line as sh -c runs it. An unquoted ( or ) ends the segment the way ; does:
// the commands of a subshell, of $(...) and <(...), and after a case pattern are segments of their
// own. $'...' is decoded as bash decodes it (macOS runs sh as bash), so $'-delete' is the word
// -delete. It returns an error on an unbalanced quote.
func Split(command string) (Result, error) {
	return split(command, true)
}

func split(command string, shell bool) (Result, error) {
	p := &parser{in: []rune(command), shell: shell}
	if err := p.run(); err != nil {
		return Result{}, err
	}
	return p.res, nil
}

// Words splits an argument string into words with the same quoting rules, ignoring operators. The
// words are a tool's arguments, never run by a shell, so parentheses and $'...' stay literal.
func Words(args string) ([]string, error) {
	res, err := split(args, false)
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
	if p.shell {
		if handled, err := p.shellStep(c); handled {
			return err
		}
	}
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
		p.hasWord = true
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

// shellStep handles what only a shell reads: the grouping operators ( and ), bash's $'...' and the
// translated $"..." (unseen). handled is false for any other rune.
func (p *parser) shellStep(c rune) (handled bool, err error) {
	switch {
	case c == '$' && p.next() == '\'':
		return true, p.ansiC()
	case c == '$' && p.next() == '"':
		p.res.Substitution = true // $"..." is translated through the locale's message catalog
		p.pos++
		return true, nil
	case c == '(' || c == ')':
		p.group(c)
		return true, nil
	}
	return false, nil
}

// next is the rune after the current one, or 0 at the end.
func (p *parser) next() rune {
	if p.pos+1 < len(p.in) {
		return p.in[p.pos+1]
	}
	return 0
}

// ansiC reads a $'...' string as bash decodes it: backslash escapes for control characters, octal,
// hexadecimal and Unicode code points, \' for a quote. The decoded text joins the word. A NUL (\x00,
// \0, \c@, a NUL code point) ends the decoded string as bash does: the rest of the $'...' is scanned
// to the closing quote but not emitted, and the word continues with whatever is glued after it.
func (p *parser) ansiC() error {
	p.pos += 2 // $'
	p.ansiStopped = false
	for p.pos < len(p.in) {
		c := p.in[p.pos]
		switch {
		case c == '\'':
			p.hasWord = true
			p.pos++
			return nil
		case c == '\\' && p.pos+1 < len(p.in):
			p.pos++
			p.escape()
		default:
			p.ansiWrite(c)
			p.pos++
		}
	}
	return errors.New("unbalanced quote")
}

// ansiWrite adds a decoded rune of a $'...' string to the word, unless a NUL has already ended it. A
// NUL is not written: bash cannot hold it in a word and truncates the ANSI-C string there.
func (p *parser) ansiWrite(r rune) {
	if p.ansiStopped {
		return
	}
	if r == 0 {
		p.ansiStopped = true
		return
	}
	p.word.WriteRune(r)
}

// ansiEscapes are the one-letter escapes of $'...'.
var ansiEscapes = map[rune]rune{'a': '\a', 'b': '\b', 'e': 0x1b, 'E': 0x1b, 'f': '\f', 'n': '\n', 'r': '\r',
	't': '\t', 'v': '\v', '\\': '\\', '\'': '\'', '"': '"', '?': '?'}

// escape decodes the escape at the current position (after the backslash) into the word and
// advances past it; an escape bash does not know stays a backslash and the character.
func (p *parser) escape() {
	c := p.in[p.pos]
	p.pos++
	if r, ok := ansiEscapes[c]; ok {
		p.ansiWrite(r)
		return
	}
	switch {
	case c == 'x':
		p.ansiWrite(p.codePoint(16, 2))
	case c == 'u':
		p.ansiWrite(p.codePoint(16, 4))
	case c == 'U':
		p.ansiWrite(p.codePoint(16, 8))
	case c >= '0' && c <= '7':
		p.pos--
		p.ansiWrite(p.codePoint(8, 3))
	case c == 'c' && p.pos < len(p.in):
		p.pos++
		p.ansiWrite(p.in[p.pos-1] & 0x1f)
	default:
		p.ansiWrite('\\')
		p.ansiWrite(c)
	}
}

// codePoint reads up to digits digits in base and returns the character they encode.
func (p *parser) codePoint(base rune, digits int) rune {
	var r rune
	for n := 0; n < digits && p.pos < len(p.in); n++ {
		d := digitValue(p.in[p.pos])
		if d < 0 || d >= base {
			break
		}
		r = r*base + d
		p.pos++
	}
	return r
}

func digitValue(c rune) rune {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return -1
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

// group ends the segment at a ( or ). A background & or a redirect after the closing ) applies to
// the whole group: the & marks the last segment (endSegment), a redirect starts a segment of its own.
// A "(" that closes a function definition (a single name, then "()") flags the line: the body runs
// on the call, so the name can shadow any allowlisted program. The segment finalized here, if any, is
// Parenthesized when it was accumulated at a positive paren depth: the last command of a subshell's
// body, or the one right before a case arm's ")". parenDepth only rises on "(": a case pattern's ")"
// has no matching "(", so it is left at 0, not driven negative.
func (p *parser) group(c rune) {
	p.endWord()
	if c == '(' && len(p.seg.Words) == 1 && len(p.seg.Redirects) == 0 && p.nextNonSpaceIsCloseParen() {
		p.res.FunctionDef = true
	}
	if p.parenDepth > 0 {
		p.seg.Parenthesized = true
	}
	p.pos++
	p.endSegment(false)
	if c == '(' {
		p.parenDepth++
	} else if p.parenDepth > 0 {
		p.parenDepth--
	}
	p.closed = c == ')'
}

// nextNonSpaceIsCloseParen reports whether the first non-space rune after the current "(" is ")",
// so "name (" or "name(" followed by ")" is a function definition and not a subshell.
func (p *parser) nextNonSpaceIsCloseParen() bool {
	for j := p.pos + 1; j < len(p.in); j++ {
		if !unicode.IsSpace(p.in[j]) {
			return p.in[j] == ')'
		}
	}
	return false
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
// follows unless the operator already named a descriptor (2>&1). An operator followed by "(" opens
// a process substitution, <(...) or >(...), which runs a command the way $(...) does.
func (p *parser) redirect() {
	fd := ""
	switch {
	case p.hasWord && isDigits(p.word.String()):
		fd = p.word.String()
		p.word.Reset()
		p.hasWord = false
	case p.hasWord:
		// A non-numeric word glued to the operator (prog>file) is not a descriptor: flush it as an
		// ordinary word before scanning the operator, so it is never merged into the redirect target.
		p.endWord()
	}
	start := p.pos
	for p.pos < len(p.in) && strings.ContainsRune("<>&", p.in[p.pos]) {
		p.pos++
	}
	op := fd + string(p.in[start:p.pos])
	if p.pos < len(p.in) && p.in[p.pos] == '(' {
		p.res.Substitution = true
		if p.shell {
			return // <(...) or >(...): the ( opens the command it runs, not a redirect target
		}
	}
	if strings.HasSuffix(op, "&") { // descriptor duplication: the target is a number or -
		tstart := p.pos
		for p.pos < len(p.in) && (unicode.IsDigit(p.in[p.pos]) || p.in[p.pos] == '-') {
			p.pos++
		}
		if p.pos > tstart {
			p.seg.Redirects = append(p.seg.Redirects, op+string(p.in[tstart:p.pos]))
			return
		}
		// Not a descriptor: bash reads ">& word" as "stdout and stderr to the file word", so the
		// next word is this operator's target, like the target of &>.
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
	if p.shell && w == "function" && len(p.seg.Words) == 0 {
		p.res.FunctionDef = true // the bash keyword: function name { ... }
	}
	p.seg.Words = append(p.seg.Words, w)
}

func (p *parser) endSegment(background bool) {
	if len(p.seg.Words) == 0 && len(p.seg.Redirects) == 0 {
		if background && p.closed && len(p.res.Segments) > 0 {
			p.res.Segments[len(p.res.Segments)-1].Background = true // (sleep 100) &
		}
		return
	}
	p.seg.Background = background
	p.res.Segments = append(p.res.Segments, p.seg)
	p.seg = Segment{}
	p.closed = false
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
