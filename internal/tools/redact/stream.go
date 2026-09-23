package redact

import (
	"bytes"
	"io"
	"sync"
)

// LineWriter passes what is written to it on to another writer redacted, a line at a time, for
// output shown while it is produced: it holds the text up to each newline, redacts the complete
// lines and writes them, and Flush writes the redacted rest. A secret that spans lines (a PEM
// private key block) is redacted where the whole output is, in the tool result, not in the live
// copy. The live copy is not counted.
type LineWriter struct {
	mu  sync.Mutex
	r   *Redactor
	w   io.Writer
	buf []byte
}

// NewLineWriter returns a LineWriter that redacts with r and writes to w.
func NewLineWriter(r *Redactor, w io.Writer) *LineWriter { return &LineWriter{r: r, w: w} }

// Write takes all of p and writes the lines it completes.
func (l *LineWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf = append(l.buf, p...)
	end := bytes.LastIndexByte(l.buf, '\n')
	if end < 0 {
		return len(p), nil
	}
	lines := string(l.buf[:end+1])
	l.buf = append(l.buf[:0], l.buf[end+1:]...)
	if _, err := io.WriteString(l.w, l.r.redact(lines, false)); err != nil {
		return len(p), err
	}
	return len(p), nil
}

// Flush redacts and writes the text held since the last newline.
func (l *LineWriter) Flush() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buf) == 0 {
		return nil
	}
	rest := string(l.buf)
	l.buf = l.buf[:0]
	_, err := io.WriteString(l.w, l.r.redact(rest, false))
	return err
}
