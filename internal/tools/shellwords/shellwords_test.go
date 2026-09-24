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

// TestSplitFlagsFunctionDefinitions (pre-tag round 2, item 1): the shell runs a function's body on
// the call, so a definition can shadow any read-only name; Split flags the three forms (name(),
// name (), function name). A subshell, a case pattern, "if ("... and quoted parentheses are not
// definitions, and Words (the dedicated tools' arguments) never runs a shell, so it flags none.
func TestSplitFlagsFunctionDefinitions(t *testing.T) {
	defs := []string{
		`ls() { find . "$@"; }; ls -delete`,
		`ls () { find . "$@"; }; ls -delete`,
		`ls() ( find . "$@" ); ls -delete`,
		`echo() { find . $*; }; echo -delete`,
		`grep() { find "$@"; }; grep . -delete`,
		`function ff { find . -delete; }; ff`,
		`function ff() { find . -delete; }; ff`,
		`a && b() { rm x; }; b`,
	}
	for _, c := range defs {
		if res, err := Split(c); err != nil || !res.FunctionDef {
			t.Errorf("%q must flag a function definition: %+v %v", c, res, err)
		}
	}
	notDefs := []string{
		`(ls -la)`, `(a; b) | c`, `if (ls); then echo yes; fi`, `time ( ls )`, `[ -f x ] && (rm y)`,
		`echo "(hi)"`, `grep -E '(foo|bar)' f`, `case x in a) ls;; esac`, `arr=(a b); echo x`,
		`echo $(ls)`, `ls -la`, `echo function here`,
	}
	for _, c := range notDefs {
		if res, err := Split(c); err != nil || res.FunctionDef {
			t.Errorf("%q must not flag a function definition: %+v %v", c, res, err)
		}
	}
}

// TestSplitTruncatesANSICAtNUL (pre-tag round 2, item 2): bash stops a $'...' string at the first
// NUL (\x00, \0, \c@, a NUL code point) but continues the word with text glued after the closing
// quote, so the decoded word is what runs. Verified against /bin/sh: $'-del\x00'ete is -delete
// (deletes), $'-del\x00ete' is -del (does not), and kube-system$'\x00' is kube-system.
func TestSplitTruncatesANSICAtNUL(t *testing.T) {
	cases := []struct{ in, want string }{
		{`find . $'-delete\x00'`, "find|.|-delete"},
		{`find . $'-delete\0'`, "find|.|-delete"},
		{`find . $'-delete\c@'`, "find|.|-delete"},
		{`find . $'-delete\U00000000'`, "find|.|-delete"},
		{`find . $'-del\x00'ete`, "find|.|-delete"},
		{`find . $'-del\x00ete'`, "find|.|-del"},
		{`echo kube-system$'\x00'`, "echo|kube-system"},
		{`echo $'kube-system\x00'`, "echo|kube-system"},
		{`echo a$'\x00'b`, "echo|ab"},
		{`echo x$'\x00'`, "echo|x"},
	}
	for _, c := range cases {
		res, err := Split(c.in)
		if err != nil || len(res.Segments) != 1 {
			t.Fatalf("%q: %+v %v", c.in, res, err)
		}
		if got := strings.Join(res.Segments[0].Words, "|"); got != c.want {
			t.Errorf("%q: words %q, want %q", c.in, got, c.want)
		}
	}
	// A $'...' that is only a NUL still produces an (empty) word.
	if res, _ := Split(`echo $'\x00'`); len(res.Segments) != 1 || len(res.Segments[0].Words) != 2 ||
		res.Segments[0].Words[1] != "" {
		t.Errorf(`echo $'\x00' must keep an empty word: %+v`, res)
	}
}

// TestSplitCapturesSubstitutionBodies (Task 6): Split records the body of every $(...) in order,
// outermost first, so the classifier can decide whether each one reads; a backtick sets Backtick and
// its body is not captured. The bodies keep their own quoting.
func TestSplitCapturesSubstitutionBodies(t *testing.T) {
	res, err := Split(`for p in $(ls -1 "$HOME"); do echo $p $(date +%F); done`)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Substitution || res.Backtick {
		t.Fatalf("%+v", res)
	}
	if got := strings.Join(res.Substitutions, "|"); got != `ls -1 "$HOME"|date +%F` {
		t.Fatalf("bodies %q", got)
	}
	res, _ = Split("echo `date`")
	if !res.Substitution || !res.Backtick || len(res.Substitutions) != 0 {
		t.Fatalf("backtick %+v", res)
	}
	res, _ = Split("echo $(echo $(id))")
	if got := strings.Join(res.Substitutions, "|"); got != "echo $(id)|id" {
		t.Fatalf("nested %q", got)
	}
}

// TestSplitCaseSubstitutionCapturesToEnd (Task 6 fix, ruling P3-R17): a case statement's arm
// terminator ")" has no matching "(", so the paren scanner would stop early and under-capture the
// body. captureSubstitution treats the "case" keyword (a whole word at a token boundary, followed by
// whitespace) as a shape it cannot follow and captures to the end of the input, so a quoted
// substitution hiding a command after a case arm classifies as a mutation, not a false read. Words
// named "case" that are not the keyword (showcase, case_x) do not trigger it.
func TestSplitCaseSubstitutionCapturesToEnd(t *testing.T) {
	res, _ := Split(`echo "$(case a in a) rm x;; esac)"`)
	if len(res.Substitutions) != 1 || res.Substitutions[0] != `case a in a) rm x;; esac)"` {
		t.Fatalf("case body must capture to the end: %q", res.Substitutions)
	}
	res, _ = Split(`echo $(case $y in x) rm -rf pwned ;; *) : ;; esac)`)
	if len(res.Substitutions) != 1 || res.Substitutions[0] != `case $y in x) rm -rf pwned ;; *) : ;; esac)` {
		t.Fatalf("unquoted case body must capture to the end: %q", res.Substitutions)
	}
	// A quoted "case" inside the body is data, not the keyword, so the paren scan still closes normally.
	res, _ = Split("echo \"$(grep 'case' f)\"")
	if len(res.Substitutions) != 1 || res.Substitutions[0] != `grep 'case' f` {
		t.Fatalf("quoted case is not the keyword: %q", res.Substitutions)
	}
	// showcase and case_x are not the keyword: the substitution closes at its own ).
	for _, c := range []struct{ in, body string }{
		{"echo $(showcase list)", "showcase list"},
		{"echo $(case_x=1 env)", "case_x=1 env"},
	} {
		if res, _ := Split(c.in); len(res.Substitutions) != 1 || res.Substitutions[0] != c.body {
			t.Errorf("%q: body %q, want %q", c.in, res.Substitutions, c.body)
		}
	}
}

// TestSplitCommentAfterParenCaptureSubstitution (Task 6 fix round 2, ruling P3-R23): a "#" glued
// right after a subshell's ")" starts a comment, and the ")" that the comment then contains must not
// close an enclosing substitution early. The scanner skips the comment to the newline and captures
// past it to the real closing paren, so a command hidden after the comment reaches the classifier.
func TestSplitCommentAfterParenCaptureSubstitution(t *testing.T) {
	res, err := Split("echo \"$( (true)#x )\nls)\"")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Substitutions) != 1 || res.Substitutions[0] != " (true)#x )\nls" {
		t.Fatalf("body must capture past the glued comment: %q", res.Substitutions)
	}
}
