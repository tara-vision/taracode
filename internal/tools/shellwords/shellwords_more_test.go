package shellwords

import (
	"reflect"
	"strings"
	"testing"
)

func TestWordsRefusesAnUnbalancedQuote(t *testing.T) {
	if w, err := Words("'x"); err == nil || err.Error() != "unbalanced quote" || w != nil {
		t.Fatalf("%q %v", w, err)
	}
}

// TestSplitWordEdges: a comment ends the line, a lone $ is a word, $'...' keeps an unknown escape and
// reads both cases of hex digits, a double-quoted \" is a quote, a backtick inside double quotes is a
// substitution, &> redirects both streams, and an empty quoted word before a redirect stays a word.
func TestSplitWordEdges(t *testing.T) {
	tests := []struct {
		line      string
		words     []string
		redirects []string
	}{
		{"ls # list files", []string{"ls"}, nil},
		{"echo $", []string{"echo", "$"}, nil},
		{`echo $'\q'`, []string{"echo", `\q`}, nil},
		{`echo $'\x4A\x4a'`, []string{"echo", "JJ"}, nil},
		{`echo "a\"b"`, []string{"echo", `a"b`}, nil},
		{"ls &> out.log", []string{"ls"}, []string{"&> out.log"}},
		{`echo "">out`, []string{"echo", ""}, []string{"> out"}},
		{"f (", []string{"f"}, nil},
	}
	for _, tt := range tests {
		res, err := Split(tt.line)
		if err != nil || len(res.Segments) != 1 || !reflect.DeepEqual(res.Segments[0].Words, tt.words) ||
			strings.Join(res.Segments[0].Redirects, ",") != strings.Join(tt.redirects, ",") || res.FunctionDef {
			t.Errorf("%q: %+v %v", tt.line, res, err)
		}
	}
	res, err := Split("echo \"`date`\"")
	if err != nil || !res.Substitution || !res.Backtick {
		t.Fatalf("a backtick inside double quotes: %+v %v", res, err)
	}
}

// TestSplitCapturesSubstitutionBodiesPastLiteralParens: a ) escaped, quoted, inside ${...} or glued
// into a word does not close the body; a heredoc, a comment or a missing ) captures to the end, which
// only ever makes the body look bigger.
func TestSplitCapturesSubstitutionBodiesPastLiteralParens(t *testing.T) {
	tests := []struct{ line, body string }{
		{`echo $(echo \))`, `echo \)`},
		{`echo $(echo $'a)b')`, `echo $'a)b'`},
		{"echo $(echo ${x})", "echo ${x}"},
		{`echo $(echo "a\")b")`, `echo "a\")b"`},
		{"echo $(echo ${a:-${b}})", "echo ${a:-${b}}"},
		{"echo $(echo ${a", "echo ${a"},
		{"echo $(showcase x)", "showcase x"},
		{"echo $(echo a#b)", "echo a#b"},
		{"echo $(date # c)", "date # c)"},
		{"echo $(date", "date"},
		{"echo $(cat <<EOF\nx\nEOF\n)", "cat <<EOF\nx\nEOF\n)"},
		{"echo $(true; case x in x) echo hit;; esac)", "true; case x in x) echo hit;; esac)"},
	}
	for _, tt := range tests {
		res, err := Split(tt.line)
		if err != nil || !res.Substitution || len(res.Substitutions) != 1 || res.Substitutions[0] != tt.body {
			t.Errorf("%q: %q %v", tt.line, res.Substitutions, err)
		}
	}
	if _, err := Split("echo $(echo 'abc"); err == nil {
		t.Fatal("an unbalanced quote inside a substitution is still an unbalanced quote")
	}
}
