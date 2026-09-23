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

// TestSplitOutputDuplicationToAWordIsAFileRedirect: bash (macOS /bin/sh) reads `>& word` and
// `>&word`, where word is not a descriptor number or -, as "stdout and stderr to the file word", so
// the word is the redirect's target, not an argument of the command.
func TestSplitOutputDuplicationToAWordIsAFileRedirect(t *testing.T) {
	cases := []struct{ in, redirects, words string }{
		{"echo x >& out.txt", ">& out.txt", "echo x"},
		{"echo x >&out.txt", ">& out.txt", "echo x"},
		{"echo x 2>&1", "2>&1", "echo x"},
		{"echo x >&2", ">&2", "echo x"},
		{"echo x >&-", ">&-", "echo x"},
	}
	for _, c := range cases {
		res, err := Split(c.in)
		if err != nil || len(res.Segments) != 1 {
			t.Fatalf("%q: %+v %v", c.in, res, err)
		}
		seg := res.Segments[0]
		if got := strings.Join(seg.Redirects, ","); got != c.redirects {
			t.Errorf("%q: redirects %q, want %q", c.in, got, c.redirects)
		}
		if got := strings.Join(seg.Words, " "); got != c.words {
			t.Errorf("%q: words %q, want %q", c.in, got, c.words)
		}
	}
}

// TestSplitFlagsProcessSubstitution: <(...) and >(...) run a command the way $(...) does when sh
// is bash 5.1 or later, so they set Substitution.
func TestSplitFlagsProcessSubstitution(t *testing.T) {
	for _, c := range []string{"diff <(rm -rf x) y", "tee >(sh) < in.txt", "cat <(id)"} {
		if res, _ := Split(c); !res.Substitution {
			t.Errorf("process substitution not detected in %q", c)
		}
	}
	if res, _ := Split("sort < in.txt > out.txt"); res.Substitution {
		t.Error("plain redirects are not a substitution")
	}
}
