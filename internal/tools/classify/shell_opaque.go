package classify

import (
	"path"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools/shellwords"
)

// scriptPrograms take a program text that can write any file it names.
var scriptPrograms = map[string]bool{"awk": true, "gawk": true, "mawk": true, "nawk": true, "sed": true}

// opaqueSegments reports a segment whose effects its words do not show, beyond what the kube pass marks
// (a substitution, a function, a program without a rule or one that runs code, a container): xargs or
// parallel in front of a program, which add its arguments at run time, and an awk or sed program text that
// writes.
func opaqueSegments(segments []shellwords.Segment) bool {
	for _, seg := range segments {
		simple, _ := simpleCommand(seg.Words)
		words := simple[assignmentsEnd(simple):]
		if len(words) == 0 {
			continue
		}
		w := unwrap(words)
		if w.feedsArguments() {
			return true
		}
		if len(w.words) > 0 && scriptPrograms[path.Base(w.words[0])] &&
			shellProgram(w.words).Classification == policy.Mutate {
			return true
		}
	}
	return false
}

// outsideToken reports a character that cannot be part of a program name, a host or a file name piece.
func outsideToken(r rune) bool {
	return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-')
}
