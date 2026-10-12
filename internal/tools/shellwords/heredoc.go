package shellwords

import (
	"errors"
	"strings"
)

// heredoc is a here-document whose body starts after the next newline: the segment that declared it, its
// delimiter, whether <<- strips the body's leading tabs, and whether the shell expands the body (an unquoted
// delimiter).
type heredoc struct {
	segment   int
	delimiter string
	stripTabs bool
	expands   bool
}

// readHeredocs reads the bodies of the here-documents declared before the newline just passed: each is the
// lines up to its delimiter line, attached to the segment that declared it, so a body is never read as commands;
// a body without its delimiter line is an error. The shell expands a body whose delimiter is unquoted, so the
// command substitutions in it are the line's own.
func (p *parser) readHeredocs() error {
	for _, h := range p.heredocs {
		var body strings.Builder
		closed, bodyStart := false, p.pos
		for p.pos < len(p.in) && !closed {
			end := skipToNewline(p.in, p.pos)
			line := string(p.in[p.pos:end])
			p.pos = min(end+1, len(p.in))
			if h.stripTabs {
				line = strings.TrimLeft(line, "\t")
			}
			if closed = line == h.delimiter; !closed {
				body.WriteString(line + "\n")
			}
		}
		if !closed {
			return errNoDelimiter
		}
		if h.expands {
			p.scanExpansions([]rune(body.String()), bodyStart >= p.outerEnd)
		}
		seg := &p.res.Segments[h.segment]
		seg.Heredocs = append(seg.Heredocs, body.String())
	}
	p.heredocs = nil
	return nil
}

// heredocOperator reports a here-document operator, << after an optional descriptor; <<< is a here-string.
func heredocOperator(op string) bool {
	return strings.HasSuffix(op, "<<") && !strings.HasSuffix(op, "<<<")
}

// errNoDelimiter refuses a here-document whose delimiter line never comes: the parser cannot tell where its body
// ends, so it does not read the rest of the line as data.
var errNoDelimiter = errors.New("a here-document has no delimiter line")
