package shellwords

import (
	"strings"
	"testing"
)

func TestSplitSegmentsAndQuotes(t *testing.T) {
	res, err := Split(`grep -n "hello world" 'it''s' file.txt | sort -u && echo done; ls`)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Segments) != 4 {
		t.Fatalf("segments %d: %+v", len(res.Segments), res.Segments)
	}
	if got := strings.Join(res.Segments[0].Words, "|"); got != "grep|-n|hello world|its|file.txt" {
		t.Errorf("words %q", got)
	}
	if res.Segments[1].Words[0] != "sort" || res.Segments[2].Words[0] != "echo" || res.Segments[3].Words[0] != "ls" {
		t.Errorf("segments %+v", res.Segments)
	}
}

func TestSplitRedirectsBackgroundAndSubstitution(t *testing.T) {
	res, err := Split(`make build 2>&1 > out.log`)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(res.Segments[0].Redirects, ","); got != "2>&1,> out.log" {
		t.Errorf("redirects %q", got)
	}
	if got := strings.Join(res.Segments[0].Words, " "); got != "make build" {
		t.Errorf("words %q", got)
	}
	res, _ = Split(`sleep 10 &`)
	if !res.Segments[0].Background {
		t.Error("background not detected")
	}
	for _, c := range []string{"echo $(whoami)", "echo `id`", `echo "$(id)"`} {
		res, _ = Split(c)
		if !res.Substitution {
			t.Errorf("substitution not detected in %q", c)
		}
	}
	if _, err := Split(`echo "unterminated`); err == nil {
		t.Error("unbalanced quote must error")
	}
}

// TestSplitGluedRedirects is a regression test for a bug where a redirect operator immediately
// following a non-numeric word (no whitespace) left that word inside the builder instead of flushing
// it, so it silently merged into the redirect's target word and vanished from Segment.Words.
func TestSplitGluedRedirects(t *testing.T) {
	cases := []struct {
		in        string
		redirects string
	}{
		{"prog>file", "> file"},
		{"prog<file", "< file"},
		{"prog>>file", ">> file"},
	}
	for _, c := range cases {
		res, err := Split(c.in)
		if err != nil {
			t.Fatalf("%q: %v", c.in, err)
		}
		if len(res.Segments) != 1 {
			t.Fatalf("%q: segments %d: %+v", c.in, len(res.Segments), res.Segments)
		}
		seg := res.Segments[0]
		if got := strings.Join(seg.Words, "|"); got != "prog" {
			t.Errorf("%q: words %q, want \"prog\"", c.in, got)
		}
		if got := strings.Join(seg.Redirects, ","); got != c.redirects {
			t.Errorf("%q: redirects %q, want %q", c.in, got, c.redirects)
		}
	}
}

func TestWordsKeepsEmptyQuotedArgs(t *testing.T) {
	w, err := Words(`commit -m "" --allow-empty`)
	if err != nil || len(w) != 4 || w[2] != "" {
		t.Fatalf("%v %v", w, err)
	}
	w, _ = Words(`log --format='%h %s' -n 3`)
	if len(w) != 4 || w[1] != "--format=%h %s" {
		t.Fatalf("%q", w)
	}
}
