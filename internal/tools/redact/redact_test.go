package redact

import (
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
