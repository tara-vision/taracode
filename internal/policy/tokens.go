package policy

import "strings"

// commandTokens are the words of a command and of its lines (the forms the classifier parsed, with their
// quotes removed, so acme-prod-'acct' reads as acme-prod-acct), and the pieces of each word split at every
// character outside [A-Za-z0-9._-], so a value glued to a flag or an assignment, a user name before a host and
// a URL's host are candidates of their own for the account and host patterns. A fully qualified name's
// trailing dot is dropped.
func commandTokens(command string, lines ...string) []string {
	var out []string
	for _, text := range append([]string{command}, lines...) {
		for _, word := range strings.Fields(text) {
			for _, token := range append([]string{word}, strings.FieldsFunc(word, outsideName)...) {
				out = append(out, token)
				if trimmed := strings.TrimSuffix(token, "."); trimmed != token {
					out = append(out, trimmed)
				}
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
