package shellwords

import (
	"reflect"
	"slices"
	"testing"
)

// TestSplitReadsHereDocumentBodies: the lines of a here-document up to its delimiter are its body, attached to
// the command that declared it, and never commands of the line; the line goes on after the delimiter.
func TestSplitReadsHereDocumentBodies(t *testing.T) {
	tests := []struct {
		name, line string
		words      [][]string
		heredocs   [][]string
	}{
		{"a commit message", "git commit -F - <<'EOF'\nbump terraform.tfstate handling\nEOF",
			[][]string{{"git", "commit", "-F", "-"}}, [][]string{{"bump terraform.tfstate handling\n"}}},
		{"a quote in the body, a command after it", "cat <<EOF\nDon't stop\nEOF\necho done",
			[][]string{{"cat"}, {"echo", "done"}}, [][]string{{"Don't stop\n"}, nil}},
		{"<<- strips leading tabs", "cat <<-EOF\n\tone\n\t\ttwo\n\tEOF\nls",
			[][]string{{"cat"}, {"ls"}}, [][]string{{"one\ntwo\n"}, nil}},
		{"two in one command", "cat <<A <<B\na\nA\nb\nB",
			[][]string{{"cat"}}, [][]string{{"a\n", "b\n"}}},
		{"the first command of a pipeline", "cat <<EOF | sh\nrm -rf x\nEOF",
			[][]string{{"cat"}, {"sh"}}, [][]string{{"rm -rf x\n"}, nil}},
		{"an empty body", "cat <<EOF\nEOF",
			[][]string{{"cat"}}, [][]string{{""}}},
		{"a here-string is not one", `sh <<< "rm -rf x"`,
			[][]string{{"sh"}}, [][]string{nil}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := Split(tt.line)
			if err != nil || len(res.Segments) != len(tt.words) {
				t.Fatalf("%+v %v", res, err)
			}
			for i, seg := range res.Segments {
				if !reflect.DeepEqual(seg.Words, tt.words[i]) || !reflect.DeepEqual(seg.Heredocs, tt.heredocs[i]) {
					t.Errorf("segment %d: words %q heredocs %q", i, seg.Words, seg.Heredocs)
				}
			}
		})
	}
}

// TestSplitExpandsAnUnquotedHereDocument: the shell expands the body of a here-document whose delimiter is not
// quoted, so its command substitutions are the line's; a quoted delimiter, or an escaped $, keeps the body literal.
func TestSplitExpandsAnUnquotedHereDocument(t *testing.T) {
	res, err := Split("cat <<EOF\nnow $(rm -rf infra)\nEOF")
	if err != nil || !res.Substitution || !reflect.DeepEqual(res.Substitutions, []string{"rm -rf infra"}) {
		t.Errorf("unquoted: %+v %v", res, err)
	}
	if res, err := Split("cat <<EOF\nnow `date`\nEOF"); err != nil || !res.Substitution || !res.Backtick {
		t.Errorf("a backtick: %+v %v", res, err)
	}
	for _, line := range []string{"cat <<'EOF'\n$(rm -rf infra)\nEOF", "cat <<\"EOF\"\n`date`\nEOF",
		"cat <<\\EOF\n$(date)\nEOF", "cat <<E'O'F\n$(date)\nEOF", "cat <<EOF\n\\$(date) \\`date\\`\nEOF"} {
		if res, err := Split(line); err != nil || res.Substitution || len(res.Substitutions) != 0 {
			t.Errorf("%q: %+v %v", line, res, err)
		}
	}
}

// TestWordsKeepsTheLinesAfterADoubleLessThan: a tool's arguments are not run by a shell, so a << there reads
// no here-document.
func TestWordsKeepsTheLinesAfterADoubleLessThan(t *testing.T) {
	if w, err := Words("a <<EOF\nb\nEOF"); err != nil || !reflect.DeepEqual(w, []string{"a", "b", "EOF"}) {
		t.Errorf("%q %v", w, err)
	}
}

// TestSplitMarksASegmentThatPipesItsOutput: a segment ended by | hands its output to the next one; || does not.
func TestSplitMarksASegmentThatPipesItsOutput(t *testing.T) {
	res, err := Split("cat <<EOF | sh\nx\nEOF\na || b; c")
	if err != nil || len(res.Segments) != 5 {
		t.Fatalf("%+v %v", res, err)
	}
	for i, want := range []bool{true, false, false, false, false} {
		if res.Segments[i].Piped != want {
			t.Errorf("segment %d %q: piped %v", i, res.Segments[i].Words, res.Segments[i].Piped)
		}
	}
}

// TestSplitCapturesBacktickBodies: a backtick substitution's command is captured as a $(...) body is, with the
// backslashes before a $, a backtick or a backslash removed, in a word, in double quotes and in an expanded
// here-document; its closing backtick opens nothing.
func TestSplitCapturesBacktickBodies(t *testing.T) {
	tests := []struct {
		line string
		want []string
	}{
		{"echo `rm -rf infra` and `date`", []string{"rm -rf infra", "date"}},
		{"echo \"`date`\"", []string{"date"}},
		{"echo `echo \\`date\\``", []string{"echo `date`"}},
		{"echo `unclosed", []string{"unclosed"}},
		{"cat <<EOF\nnow `rm -rf infra`\nEOF", []string{"rm -rf infra"}},
	}
	for _, tt := range tests {
		res, err := Split(tt.line)
		if err != nil || !res.Backtick || !reflect.DeepEqual(res.Substitutions, tt.want) {
			t.Errorf("%q: %+v %v", tt.line, res, err)
		}
	}
}

// TestSplitReadsArithmeticAndParameterExpansionsAsWords: a << inside $((...)), $[...], ${...} or an arithmetic
// command ((...)) is a shift or a pattern, not a here-document, so the lines after it are still commands; the
// command substitutions inside such an expansion are captured.
func TestSplitReadsArithmeticAndParameterExpansionsAsWords(t *testing.T) {
	tests := []struct {
		line  string
		words [][]string
	}{
		{"echo $((1<<20))\nterraform destroy", [][]string{{"echo", "$((1<<20))"}, {"terraform", "destroy"}}},
		{"echo $[1<<2]\nrm -rf .git/config", [][]string{{"echo", "$[1<<2]"}, {"rm", "-rf", ".git/config"}}},
		{"echo ${a:-<<x y}\nrm x", [][]string{{"echo", "${a:-<<x y}"}, {"rm", "x"}}},
		{"(( x = 1<<2 ))\nrm x", [][]string{{"(( x = 1<<2 ))"}, {"rm", "x"}}},
		{"echo \"$((1<<20))\"\nrm x", [][]string{{"echo", "$((1<<20))"}, {"rm", "x"}}},
		{"((echo a); echo b)", [][]string{{"echo", "a"}, {"echo", "b"}}},
		{`echo ${a:-"}"} ${a:-'}'} ${a:-\}} ${a:-` + "`echo }`" + `}`,
			[][]string{{"echo", `${a:-"}"}`, `${a:-'}'}`, `${a:-\}}`, "${a:-`echo }`}"}}},
	}
	for _, tt := range tests {
		res, err := Split(tt.line)
		if err != nil || len(res.Segments) != len(tt.words) {
			t.Errorf("%q: %+v %v", tt.line, res, err)
			continue
		}
		for i, seg := range res.Segments {
			if !reflect.DeepEqual(seg.Words, tt.words[i]) || len(seg.Heredocs) != 0 {
				t.Errorf("%q segment %d: words %q heredocs %q", tt.line, i, seg.Words, seg.Heredocs)
			}
		}
	}
	for line, body := range map[string]string{"echo $(( $(rm -rf infra) + 1 ))": "rm -rf infra",
		"echo ${a:-$(rm -rf infra)}": "rm -rf infra", "echo $((`rm -rf infra` + 1))": "rm -rf infra",
		"echo $((echo a); echo b)": "(echo a); echo b", "echo $(echo $((1<<2)))": "echo $((1<<2))",
		"echo $(echo $((a)+(b)))": "echo $((a)+(b))"} {
		if res, err := Split(line); err != nil || !res.Substitution || !slices.Contains(res.Substitutions, body) {
			t.Errorf("%q: %+v %v", line, res, err)
		}
	}
}

// TestSplitRefusesWhatItCannotClose: a here-document without its delimiter line and an arithmetic or parameter
// expansion without its end are errors, so the classifier fails closed rather than read later lines as data.
func TestSplitRefusesWhatItCannotClose(t *testing.T) {
	for _, line := range []string{"cat <<EOF\nline\n", "cat <<EOF", "echo $((1<<20)\nterraform destroy",
		"echo $[1<<2\nrm x", "echo ${a:-<<x\nrm x", "(( x = 1<<2 )\nrm x", "echo $(echo ${a"} {
		if res, err := Split(line); err == nil {
			t.Errorf("%q: %+v, want an error", line, res)
		}
	}
}
