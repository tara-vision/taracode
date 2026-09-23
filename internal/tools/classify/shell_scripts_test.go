package classify

import "testing"

// TestSedScriptWrites reads sed scripts the way sed does: w, W and e commands and the w and e
// flags of s write or run; the same letters inside addresses, expressions, replacement text, a/i/c
// text, file names and labels do not. A script it cannot read counts as writing.
func TestSedScriptWrites(t *testing.T) {
	cases := []struct {
		script string
		writes bool
	}{
		{"p", false}, {"1,20p", false}, {"/start/,/end/p", false}, {"s/a/b/g", false}, {"s/w/W/g", false},
		{"s|/usr|/opt|g", false}, {`s/a\/w/c/`, false}, {"y/abc/xyz/", false}, {"/^#/d", false},
		{"$!N;P;D", false}, {"/x/{p;d}", false}, {"/x/I p", false}, {`\%w%p`, false}, {"0~3p", false},
		{"1,+2p", false}, {"1a hello; w not a command", false}, {"1i\\\nwrite this line\nq", false},
		{"r include.txt; w x", false}, {":loop;N;b loop", false}, {"#n comment w\np", false}, {"q5", false},
		{"l 40", false}, {"=", false}, {"s/a/b/2", false}, {"s/a/b/gip", false},
		{"w out.txt", true}, {"1,5W out", true}, {"/x/w out", true}, {"s/a/b/w out", true}, {"s/a/b/gw out", true},
		{"s/x/date/e", true}, {"e date", true}, {"1e", true}, {"b end; w out", true}, {"p;w out", true},
		{"/unterminated", true}, {"s/a/b", true}, {"y/ab/", true}, {"k", true}, {"5", true}, {`\%x`, true},
		{"/x/{\nw out\n}", true},
	}
	for _, c := range cases {
		if got := sedScriptWrites(c.script); got != c.writes {
			t.Errorf("sedScriptWrites(%q) = %v", c.script, got)
		}
	}
}

// TestAwkCodeOutsideLiterals finds the pipes and the @ calls awk runs, and ignores a | inside a
// string, a regular expression or a comment, telling a regular expression from a division the way
// awk's lexer does.
func TestAwkCodeOutsideLiterals(t *testing.T) {
	cases := []struct {
		program string
		code    bool
	}{
		{"{print $1}", false}, {"/error|warn/ {print}", false}, {"$0 ~ /a|b/", false}, {`{print "a|b"}`, false},
		{`{print "a\"|b"}`, false}, {`/a\/|b/`, false}, {"a || b", false}, {"# a | comment\n{print}", false},
		{"{print \"unclosed", false}, {"{n = NF / 2; print n}", false}, {"{print $1/2}", false},
		{`{print $1 | "sort"}`, true}, {`{"date" | getline d}`, true}, {`{print |& "sh"}`, true},
		{`{x = 4 / 2 | "sh"}`, true}, {`{print x++ / 2 | "sh"}`, true}, {`{print (a) /2|"sh"/ 1}`, true},
		{`BEGIN { f = "system"; @f("id") }`, true}, {`@load "x"`, true}, {"/a\nb|c/", true},
		{`{print a[1] /2| "sh"}`, true}, {`{print x-- /2| "sh"}`, true},
	}
	for _, c := range cases {
		if got := awkCodeOutsideLiterals(c.program); got != c.code {
			t.Errorf("awkCodeOutsideLiterals(%q) = %v", c.program, got)
		}
	}
}
