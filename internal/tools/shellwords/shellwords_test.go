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

// segmentsOf renders the segments of a parse: words joined by spaces, a redirect after "+", and
// "&" for a background segment; segments are joined by " | ".
func segmentsOf(t *testing.T, command string) (string, bool) {
	t.Helper()
	res, err := Split(command)
	if err != nil {
		t.Fatalf("%q: %v", command, err)
	}
	var out []string
	for _, s := range res.Segments {
		text := strings.Join(s.Words, " ")
		for _, r := range s.Redirects {
			text += " +" + r
		}
		if s.Background {
			text += " &"
		}
		out = append(out, strings.TrimSpace(text))
	}
	return strings.Join(out, " | "), res.Substitution
}

// TestSplitParenthesesSeparateCommands (pre-tag round B): an unquoted ( or ) is a shell operator, so
// a subshell, a case pattern and the command inside $(...) or <(...) are segments of their own; a
// background & or a redirect after the closing ) still reaches the classifier. Quoted or escaped
// parentheses stay in the word, and Words (the dedicated tools' arguments, never run by a shell)
// keeps them literal.
func TestSplitParenthesesSeparateCommands(t *testing.T) {
	cases := []struct {
		in, want     string
		substitution bool
	}{
		{"(ls -la)", "ls -la", false},
		{"(a; b) | c", "a | b | c", false},
		{"(sleep 100) &", "sleep 100 &", false},
		{"(ls) > out.txt", "ls | +> out.txt", false},
		{"case x in a) ls;; esac", "case x in a | ls | esac", false},
		{"echo $(kubectl delete pod x -n kube-system)", "echo $ | kubectl delete pod x -n kube-system", true},
		{"diff <(rm -rf x) y", "diff | rm -rf x | y", true},
		{"find . \\( -name a -o -name b \\)", "find . ( -name a -o -name b )", false},
		{`grep "(x)" f`, "grep (x) f", false},
		{"f() { rm x; }; f", "f | { rm x | } | f", false},
	}
	for _, c := range cases {
		got, sub := segmentsOf(t, c.in)
		if got != c.want || sub != c.substitution {
			t.Errorf("%q: segments %q (substitution %v), want %q (%v)", c.in, got, sub, c.want, c.substitution)
		}
	}
	if w, err := Words("log --format=%h(%an) -n 3"); err != nil || strings.Join(w, "|") != "log|--format=%h(%an)|-n|3" {
		t.Errorf("Words keeps parentheses literal: %q %v", w, err)
	}
}

// TestSplitDecodesANSICQuotingAndFlagsTranslation (pre-tag round B): bash decodes $'...' before the
// command sees it, so an option written $'-delete' or $'\x2ddelete' is the -delete it runs; $"..."
// is translated through the locale's message catalog, so its text is not known and counts as a
// substitution. Words, for the dedicated tools' arguments, runs no shell and keeps them literal.
func TestSplitDecodesANSICQuotingAndFlagsTranslation(t *testing.T) {
	cases := []struct{ in, want string }{
		{`find . $'-delete'`, "find|.|-delete"},
		{`find . $'\x2ddelete'`, "find|.|-delete"},
		{`grep $'a\tb' f`, "grep|a\tb|f"},
		{`echo $'it\'s' $'\101\u0042'`, "echo|it's|AB"},
		{`echo x$'\n'y`, "echo|x\ny"},
	}
	for _, c := range cases {
		res, err := Split(c.in)
		if err != nil || len(res.Segments) != 1 || res.Substitution {
			t.Fatalf("%q: %+v %v", c.in, res, err)
		}
		if got := strings.Join(res.Segments[0].Words, "|"); got != strings.NewReplacer(`\t`, "\t", `\n`, "\n").Replace(c.want) {
			t.Errorf("%q: words %q, want %q", c.in, got, c.want)
		}
	}
	if res, _ := Split(`echo $"hello"`); !res.Substitution {
		t.Error(`$"..." is translated at run time: it must count as a substitution`)
	}
	if _, err := Split(`echo $'unterminated`); err == nil {
		t.Error("an unbalanced $'...' must error")
	}
	if w, _ := Words(`-l $'a'`); strings.Join(w, "|") != "-l|$a" {
		t.Errorf("Words keeps $'...' literal: %q", w)
	}
}
