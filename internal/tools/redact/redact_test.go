package redact

import (
	"errors"
	"strings"
	"testing"
)

func TestBuiltInPatterns(t *testing.T) {
	r, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ in, want string }{
		{"key AKIAIOSFODNN7EXAMPLE here", "key [redacted:aws-access-key] here"},
		{"k: AIzaSyA-1234567890abcdefghijklmnopqrstuvw", "k: [redacted:gcp-api-key]"},
		{"AIza" + strings.Repeat("Q", 300), "AIza" + strings.Repeat("Q", 300)},
		{"token ghp_abcdefghijklmnopqrstuvwxyz0123456789 ok", "token [redacted:github-token] ok"},
		{"github_pat_11ABCDEFG0123456789_abcdefghijklmnopqrstuvwxyz", "[redacted:github-token]"},
		{"xoxb-123456789012-abcdefghijkl", "[redacted:slack-token]"},
		{"jwt eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U", "jwt [redacted:jwt]"},
		{"-----BEGIN RSA PRIVATE KEY-----\nMIIabc\n-----END RSA PRIVATE KEY-----", "[redacted:private-key]"},
		{"password=hunter22", "password=[redacted:credential]"},
		{"DB_PASSWORD: \"s3cr3tpass\"", "DB_PASSWORD: \"[redacted:credential]"},
		{"api_key = abcd1234", "api_key = [redacted:credential]"},
		{"Authorization: Bearer abc.def-ghi_jkl", "Authorization: Bearer [redacted:credential]"},
		{"postgres://user:pa55word@db:5432/x", "postgres://user:[redacted:url-password]@db:5432/x"},
		{"AccountKey=" + strings.Repeat("a", 86) + "==", "AccountKey=[redacted:azure-key]"},
		{"?sig=abcdefghijklmnopqrstuvwxyz%3D", "?sig=[redacted:azure-sas]"},
		{"the token expired; tokens are per user", "the token expired; tokens are per user"},
		{"password=abc", "password=abc"},
	}
	for _, c := range cases {
		if got := r.Redact(c.in); got != c.want {
			t.Errorf("Redact(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
	if r.Count() == 0 {
		t.Error("the counter must count redacted spans")
	}
}

func TestEnvironmentValuesAndExtraPatterns(t *testing.T) {
	r, err := New(Options{
		ExtraPatterns: []string{`ACME-[0-9]{6}`},
		Environ:       []string{"MY_API_TOKEN=tok-1234567890", "SHORT_TOKEN=abc", "HOME=/Users/x", "DB_PASSWORD=p@ssw0rd!!"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := r.Redact("using tok-1234567890 and p@ssw0rd!! at /Users/x with ACME-123456 and abc")
	want := "using [redacted:env:MY_API_TOKEN] and [redacted:env:DB_PASSWORD] at /Users/x with [redacted:custom] and abc"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
	if _, err := New(Options{ExtraPatterns: []string{"("}}); err == nil {
		t.Error("an invalid extra pattern must be an error")
	}
}

func TestCredentialPatternDoesNotReRedactAnAlreadyRedactedValue(t *testing.T) {
	r, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	got := r.Redact("secret=AKIAIOSFODNN7EXAMPLE")
	want := "secret=[redacted:aws-access-key]"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
	if r.Count() != 1 {
		t.Errorf("count = %d, want 1", r.Count())
	}
}

// TestLineWriterRedactsEachLineBeforeItReachesTheScreen: the live shell stream goes through the
// redactor a line at a time (final review I4), a line split across writes included, and Flush
// writes the redacted tail. The stream does not count: the tool result the model gets is redacted
// and counted once.
func TestLineWriterRedactsEachLineBeforeItReachesTheScreen(t *testing.T) {
	r, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	var screen strings.Builder
	w := NewLineWriter(r, &screen)
	for _, chunk := range []string{"ok\nkey=AKIAIOSF", "ODNN7EXAMPLE\n", "tail AKIAIOSFODNN7EXAMPLE"} {
		if n, err := w.Write([]byte(chunk)); err != nil || n != len(chunk) {
			t.Fatalf("write %q: %d %v", chunk, n, err)
		}
	}
	if got := screen.String(); got != "ok\nkey=[redacted:aws-access-key]\n" {
		t.Fatalf("complete lines are written redacted, the tail waits: %q", got)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := screen.String(); got != "ok\nkey=[redacted:aws-access-key]\ntail [redacted:aws-access-key]" {
		t.Fatalf("flush writes the redacted tail: %q", got)
	}
	if err := w.Flush(); err != nil || strings.Count(screen.String(), "tail") != 1 {
		t.Fatalf("a second flush writes nothing: %q %v", screen.String(), err)
	}
	if r.Count() != 0 {
		t.Fatalf("the live stream must not count redactions: %d", r.Count())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("screen gone") }

func TestLineWriterReportsTheScreensWriteError(t *testing.T) {
	r, _ := New(Options{})
	w := NewLineWriter(r, failingWriter{})
	if _, err := w.Write([]byte("line\n")); err == nil {
		t.Fatal("a failed write must be reported")
	}
	if _, err := w.Write([]byte("tail")); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err == nil {
		t.Fatal("a failed flush must be reported")
	}
}
