package classify

import (
	"regexp"
	"strings"
)

// sedResult: sed reads unless it edits in place (-i, BSD's -I, --in-place or any abbreviation of
// it, also inside a cluster such as -ni), runs a script file it cannot see (-f), or a script writes
// a file (w, W, the w flag of s) or runs a command (e, the e flag of s).
func sedResult(prog string, rest []string) Result {
	if sedInPlace(rest) {
		return mutate(prog, "sed -i edits files in place")
	}
	// Only -e ends a cluster: BSD sed's -l takes no value, so the f of -lf is the script file there.
	if hasGNUFlag(rest, nil, "--file") || shortFlag(rest, "f", "e") {
		return mutate(prog, "sed -f runs a script file the classifier cannot read")
	}
	for _, script := range sedScripts(rest) {
		if sedScriptWrites(script) {
			return mutate(prog, "the sed script writes a file (w) or runs a command (e)")
		}
	}
	return read(prog)
}

// sedInPlace reports -i, -I or --in-place. The cluster is read through -l, which takes a value in
// GNU sed but none in BSD sed: -li with an empty suffix is an in-place edit on macOS.
func sedInPlace(rest []string) bool {
	return hasGNUFlag(rest, nil, "--in-place") || shortFlag(rest, "iI", "ef")
}

// sedScripts returns the scripts of a sed command: every -e or --expression value, or else the first
// operand. GNU sed's -l takes a value and BSD sed's does not, so the scripts of both readings are
// returned, and a script either sed would run is checked.
func sedScripts(rest []string) []string {
	var scripts []string
	for _, valued := range []string{"l", ""} {
		found, ops := splitScripts(rest, 'e', "--expression", valued, []string{"--line-length"})
		if len(found) == 0 && len(ops) > 0 {
			found = ops[:1]
		}
		scripts = append(scripts, found...)
	}
	return scripts
}

// splitScripts separates a sed or awk command line into the scripts given with the script option
// (script as a short letter, scriptLong as a long name, which may be abbreviated) and the operands,
// skipping the value of every other option in valued or valuedLong. A lone "--" ends the options.
func splitScripts(rest []string, script rune, scriptLong, valued string, valuedLong []string) (scripts, ops []string) {
	for i := 0; i < len(rest); i++ {
		t := rest[i]
		switch {
		case t == "--":
			return scripts, append(ops, rest[i+1:]...)
		case strings.HasPrefix(t, "--"):
			name, value, hasValue := strings.Cut(t, "=")
			isScript := len(name) >= 3 && strings.HasPrefix(scriptLong, name)
			if !hasValue && (isScript || abbreviates(name, valuedLong)) && i+1 < len(rest) {
				i++
				value = rest[i]
			}
			if isScript {
				scripts = append(scripts, value)
			}
		case len(t) > 1 && t[0] == '-':
			for j, c := range t[1:] {
				if c != script && !strings.ContainsRune(valued, c) {
					continue
				}
				value := t[j+2:]
				if value == "" && i+1 < len(rest) {
					i++
					value = rest[i]
				}
				if c == script {
					scripts = append(scripts, value)
				}
				break
			}
		default:
			ops = append(ops, t)
		}
	}
	return scripts, ops
}

// abbreviates reports whether name is one of the long options or an abbreviation of one.
func abbreviates(name string, long []string) bool {
	for _, l := range long {
		if len(name) >= 3 && strings.HasPrefix(l, name) {
			return true
		}
	}
	return false
}

// sedScanner reads a sed script command by command.
type sedScanner struct {
	s   string
	pos int
}

func (p *sedScanner) done() bool { return p.pos >= len(p.s) }

func (p *sedScanner) skip(set string) {
	for !p.done() && strings.IndexByte(set, p.s[p.pos]) >= 0 {
		p.pos++
	}
}

// skipUntil advances to the next byte in stops (a newline always stops).
func (p *sedScanner) skipUntil(stops string) {
	for !p.done() && p.s[p.pos] != '\n' && strings.IndexByte(stops, p.s[p.pos]) < 0 {
		p.pos++
	}
}

// skipTo advances past the next unescaped delim; false when there is none. With brackets, as in a
// regular expression, a bracket expression is skipped whole, so a delimiter inside it ([/], [^/])
// does not end the expression (BSD sed, the sed of macOS, reads it so; GNU sed refuses it).
func (p *sedScanner) skipTo(delim byte, brackets bool) bool {
	for !p.done() {
		c := p.s[p.pos]
		switch {
		case c == '\\':
			p.pos += 2
		case brackets && c == '[' && c != delim:
			if !p.skipBracket() {
				return false
			}
		case c == delim:
			p.pos++
			return true
		default:
			p.pos++
		}
	}
	return false
}

// skipBracket advances past the bracket expression that opens at pos: a ] right after [ or [^ is
// literal, and [:class:], [.coll.] and [=equiv=] may hold a ]. false when it is not closed.
func (p *sedScanner) skipBracket() bool {
	i := p.pos + 1
	if i < len(p.s) && p.s[i] == '^' {
		i++
	}
	if i < len(p.s) && p.s[i] == ']' {
		i++
	}
	for i < len(p.s) && p.s[i] != '\n' {
		switch {
		case p.s[i] == ']':
			p.pos = i + 1
			return true
		case p.s[i] == '[' && i+1 < len(p.s) && strings.IndexByte(".:=", p.s[i+1]) >= 0:
			end := strings.Index(p.s[i+2:], string(p.s[i+1])+"]")
			if end < 0 {
				return false
			}
			i += end + 4
		default:
			i++
		}
	}
	return false
}

// skipParts skips the two parts closed by the delimiter at pos: s/re/rep/ (regex: a bracket
// expression protects the delimiter in re) and y/src/dst/.
func (p *sedScanner) skipParts(regex bool) bool {
	if p.done() {
		return false
	}
	delim := p.s[p.pos]
	p.pos++
	return p.skipTo(delim, regex) && p.skipTo(delim, false)
}

// skipAddresses advances past line numbers, $, /re/ and \cREc addresses with their I and M flags,
// the ~ + and , between them and ! negation. false when a regular expression is not closed.
func (p *sedScanner) skipAddresses() bool {
	for {
		p.skip(" \t0123456789$,~+!")
		if p.done() {
			return true
		}
		switch p.s[p.pos] {
		case '/':
			p.pos++
			if !p.skipTo('/', true) {
				return false
			}
		case '\\':
			p.pos += 2
			if p.pos > len(p.s) || !p.skipTo(p.s[p.pos-1], true) {
				return false
			}
		default:
			return true
		}
		p.skip("IM")
	}
}

// skipText advances past the text of a, i and c: the rest of the line, where a backslash escapes the
// next byte, so a\ and a line ending in a backslash continue on the next line.
func (p *sedScanner) skipText() {
	for !p.done() {
		c := p.s[p.pos]
		p.pos++
		if c == '\\' {
			p.pos++
			continue
		}
		if c == '\n' {
			return
		}
	}
}

// substituteFlagsWrite reads the flags after s///: w writes the pattern space to a file and e runs
// it as a command.
func (p *sedScanner) substituteFlagsWrite() bool {
	for !p.done() {
		c := p.s[p.pos]
		if c == 'w' || c == 'e' {
			return true
		}
		if strings.IndexByte("gpiImM0123456789", c) < 0 {
			return false
		}
		p.pos++
	}
	return false
}

// sedScriptWrites reports whether a sed script writes a file (w, W, the w flag of s) or runs a
// command (e, the e flag of s). It reads the script command by command, skipping addresses, regular
// expressions and replacement text, so the w of s/word/x/ is not a command. A script it cannot read
// (an unclosed expression, a command it does not know) counts as writing.
func sedScriptWrites(script string) bool {
	p := &sedScanner{s: script}
	for {
		p.skip(" \t\n;")
		if p.done() {
			return false
		}
		if !p.skipAddresses() || p.done() {
			return true
		}
		cmd := p.s[p.pos]
		p.pos++
		switch cmd {
		case 'w', 'W', 'e':
			return true
		case 's':
			if !p.skipParts(true) || p.substituteFlagsWrite() {
				return true
			}
		case 'y':
			if !p.skipParts(false) {
				return true
			}
		case 'a', 'i', 'c':
			p.skipText()
		case 'r', 'R', '#':
			p.skipUntil("")
		case 'b', 't', 'T', ':', 'q', 'Q', 'l', 'L', 'v':
			p.skipUntil(";}")
		case '{', '}', '=', 'd', 'D', 'g', 'G', 'h', 'H', 'n', 'N', 'p', 'P', 'x', 'z', 'F':
		default:
			return true
		}
	}
}

var awkSystem = regexp.MustCompile(`\bsystem\s*\(`)

// awkResult: awk reads unless it runs a program, a library or an extension from a file (-f, gawk's
// -E, -i and -l), writes a dump or profile file (gawk's -d, -o and -p), takes a mawk -W option, or a
// program writes a file (> after print or printf), runs a command (system, a | pipe, |&) or loads
// code (@load, @include, an @f() indirect call).
func awkResult(prog string, rest []string) Result {
	if shortFlag(rest, "fEildopW", "Fve") || hasGNUFlag(rest, nil, "--file", "--exec", "--include", "--load",
		"--dump-variables", "--pretty-print", "--profile") {
		return mutate(prog, prog+" -f and the file options run or write files the classifier cannot read")
	}
	programs, ops := splitScripts(rest, 'e', "--source", "Fv", []string{"--field-separator", "--assign"})
	if len(programs) == 0 && len(ops) > 0 {
		programs = ops[:1]
	}
	for _, program := range programs {
		if awkWritesFile(program) || awkSystem.MatchString(program) || awkCodeOutsideLiterals(program) {
			return mutate(prog, "the "+prog+" program writes files, runs commands or loads code")
		}
	}
	return read(prog)
}

// awkKeywords end no operand: after print, return or in, a / starts a regular expression.
var awkKeywords = map[string]bool{"print": true, "printf": true, "return": true, "in": true, "if": true, "while": true,
	"for": true, "do": true, "else": true, "delete": true, "exit": true, "next": true, "nextfile": true, "getline": true,
	"BEGIN": true, "END": true}

// awkWritesFile reports a > that redirects output: outside parentheses, string and regular
// expression literals and comments, not the first byte of >=, in a statement that began with print
// or printf. Anywhere else (a pattern, an if condition, an assignment) > compares.
func awkWritesFile(program string) bool {
	depth, printing, operand := 0, false, false
	word := ""
	for i := 0; i < len(program); i++ {
		c := program[i]
		if !isAwkWordByte(c) && word != "" {
			if word == "print" || word == "printf" {
				printing = true
			}
			word = ""
		}
		if end, ok := awkLiteral(program, i, operand); ok {
			i, operand = end, c != '#'
			continue
		}
		switch {
		case isAwkWordByte(c):
			word += string(c)
			operand = true
			continue
		case c == '(':
			depth++
		case c == ')':
			depth--
		case awkStatementEnds(c):
			printing = false
		case awkRedirectsAt(program, i, depth, printing):
			return true
		}
		if c != ' ' && c != '\t' {
			operand = awkEndsOperand(program, i)
		}
	}
	return false
}

// awkStatementEnds reports the bytes that end a statement, after which print or printf no longer
// governs a later >: a semicolon or a block boundary. A newline is not one: awk continues a statement
// across a backslash-newline and after a comma, && or ||, so a > on the next line still redirects the
// print. A comparison on a line after a print therefore counts as a redirect, which fails closed.
func awkStatementEnds(c byte) bool {
	return c == ';' || c == '{' || c == '}'
}

// awkRedirectsAt reports whether the byte at i is a > that redirects output: outside parentheses
// (depth 0), in a print or printf statement (printing), and not the first byte of >=.
func awkRedirectsAt(program string, i, depth int, printing bool) bool {
	return program[i] == '>' && depth == 0 && printing && (i+1 >= len(program) || program[i+1] != '=')
}

// awkCodeOutsideLiterals reports a | that is not || (a pipe to or from a command, or |&), or an @
// (gawk's @load, @include and indirect calls such as @f() with f = "system"), outside string
// literals, regular expression literals and comments. A / starts a regular expression unless it
// follows an operand (a name, a number, a closing bracket, a $ field or a postfix ++ or --), the rule
// awk's lexer uses to tell it from division; an unclosed one is read as division, so a pipe after
// it is still seen.
func awkCodeOutsideLiterals(program string) bool {
	operand := false
	word := ""
	for i := 0; i < len(program); i++ {
		c := program[i]
		if !isAwkWordByte(c) && word != "" {
			if awkKeywords[word] {
				operand = false
			}
			word = ""
		}
		if end, ok := awkLiteral(program, i, operand); ok {
			i, operand = end, c != '#'
			continue
		}
		switch {
		case c == '|' && i+1 < len(program) && program[i+1] == '|':
			i, operand = i+1, false
		case c == '|' || c == '@':
			return true
		case isAwkWordByte(c):
			word += string(c)
			operand = true
		case c != ' ' && c != '\t':
			operand = awkEndsOperand(program, i)
		}
	}
	return false
}

// awkLiteral returns the index of the last byte of the string literal, regular expression literal
// or comment that starts at i. ok is false when none starts there: a / after an operand is
// division, and so is an unclosed one.
func awkLiteral(program string, i int, operand bool) (int, bool) {
	switch program[i] {
	case '"':
		return awkLiteralEnd(program, i, '"'), true
	case '/':
		if operand {
			return i, false
		}
		end := awkLiteralEnd(program, i, '/')
		return end, end < len(program)
	case '#':
		if end := strings.IndexByte(program[i:], '\n'); end >= 0 {
			return i + end, true
		}
		return len(program), true
	}
	return i, false
}

// awkEndsOperand reports whether the byte at i ends an operand, after which a / is division: a
// name, a number, a closing bracket, a $ field, or the second + or - of a postfix ++ or --.
func awkEndsOperand(program string, i int) bool {
	c := program[i]
	if isAwkWordByte(c) || c == ')' || c == ']' || c == '$' {
		return true
	}
	return (c == '+' || c == '-') && i > 0 && program[i-1] == c
}

// awkLiteralEnd returns the index of the byte closing the string or regular expression literal that
// opens at i, or len(program) when it is not closed (a regular expression ends at a newline).
func awkLiteralEnd(program string, i int, closer byte) int {
	for j := i + 1; j < len(program); j++ {
		switch program[j] {
		case '\\':
			j++
		case closer:
			return j
		case '\n':
			if closer == '/' {
				return len(program)
			}
		}
	}
	return len(program)
}

func isAwkWordByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
