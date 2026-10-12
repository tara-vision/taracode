package policy

import "strings"

// commandTokens are the words of a command and the pieces of each word split at every character outside
// [A-Za-z0-9._-], so a value glued to a flag or an assignment, a user name before a host and a URL's host
// are candidates of their own for the account and host patterns.
func commandTokens(command string) []string {
	var out []string
	for _, word := range strings.Fields(command) {
		out = append(out, word)
		for _, piece := range strings.FieldsFunc(word, outsideName) {
			if piece != word {
				out = append(out, piece)
			}
		}
	}
	return out
}

// outsideName reports a character that cannot be part of an account, a host or a file name piece.
func outsideName(r rune) bool { return !nameRune(r) }

// nameRune reports a character of [A-Za-z0-9._-].
func nameRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-'
}
