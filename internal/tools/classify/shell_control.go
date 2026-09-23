package classify

// openingWords are the reserved words and the brace the shell reads before a command: if and elif
// and while and until before a condition, then and else and do before a body, ! before a negated
// pipeline, { before a group. closingWords end a compound command and run nothing themselves.
var (
	openingWords = map[string]bool{"!": true, "{": true, "if": true, "then": true, "else": true, "elif": true,
		"while": true, "until": true, "do": true}
	closingWords = map[string]bool{"}": true, "done": true, "fi": true, "esac": true}
)

// simpleCommand returns the command a segment runs: its words without the reserved words and the
// brace the shell reads before it. header is true for a segment that runs nothing itself: reserved
// words only (do, then, {), a closing word alone (done, fi, esac, }), or the head of a for loop or of
// a case (for f in a b, case $x in a), whose words are data. The parentheses of a subshell and of a
// case pattern never reach here: shellwords ends a segment at each.
func simpleCommand(words []string) (command []string, header bool) {
	i := 0
	for i < len(words) && openingWords[words[i]] {
		i++
	}
	command = words[i:]
	switch {
	case len(command) == 0, len(command) == 1 && closingWords[command[0]]:
		return nil, true
	case command[0] == "for" || command[0] == "case":
		return nil, true
	}
	return command, false
}

// withoutGluedBrace drops a { glued to the first word. sh runs a program named {kubectl there (a
// group needs { as a word of its own), which finds nothing; the targets and the written paths read
// the word as the program it names, so a typo never hides them. The classification keeps the word.
func withoutGluedBrace(words []string) []string {
	if len(words) == 0 || len(words[0]) < 2 || words[0][0] != '{' {
		return words
	}
	return append([]string{words[0][1:]}, words[1:]...)
}
