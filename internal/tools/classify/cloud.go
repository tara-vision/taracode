package classify

import "strings"

var cloudReadPrefixes = []string{"describe", "get", "list", "ls", "show", "search", "filter", "lookup", "query",
	"scan", "head", "batch-get", "estimate", "simulate", "test", "validate", "check", "wait", "tail", "preview",
	"select", "exists", "count", "print", "version", "help", "info", "whoami"}

// The global flags each CLI takes anywhere on the line, before the service or the verb too. A flag
// listed as taking a value is skipped together with its value; a flag listed as taking none is
// skipped alone. A flag in neither list may take a value, and that value would be read as a word of
// the command, so before the verb is known it fails the line closed (leadingWords, commandPath).
var awsValueFlags = []string{"--profile", "--region", "--output", "--query", "--endpoint-url", "--cli-connect-timeout",
	"--cli-read-timeout", "--ca-bundle", "--color", "--cli-binary-format"}
var awsBoolFlags = []string{"--debug", "--no-verify-ssl", "--no-paginate", "--no-sign-request", "--no-cli-pager",
	"--cli-auto-prompt", "--no-cli-auto-prompt", "--version"}
var azValueFlags = []string{"--subscription", "-s", "-n", "--name", "-g", "--resource-group", "-o", "--output",
	"--query", "-l", "--location"}
var azBoolFlags = []string{"--help", "-h", "--version", "--verbose", "--debug", "--only-show-errors"}
var gcloudValueFlags = []string{"--project", "--zone", "--region", "--format", "--filter", "--account",
	"--configuration", "--verbosity", "--impersonate-service-account", "--billing-project", "--access-token-file",
	"--flags-file", "--trace-token"}
var gcloudBoolFlags = []string{"--quiet", "-q", "--help", "-h", "--version", "--log-http", "--user-output-enabled",
	"--no-user-output-enabled"}

// cloudFileWriters are the verbs that look like reads but write a file: get-credentials merges a
// cluster into the kubeconfig (az, gcloud), and the AWS operations that stream their response
// into the outfile operand they require (select-object-content writes its query result there).
var cloudFileWriters = []string{"get-credentials", "get-object", "get-object-torrent", "get-job-output", "get-export",
	"get-sdk", "get-media", "get-clip", "get-snapshot-block", "get-thing-shadow", "get-raw-message-content",
	"get-package-version-asset", "select-object-content"}

// Cloud classifies a provider CLI invocation by the verb in the position each CLI uses.
func Cloud(provider string, tokens []string) Result {
	switch provider {
	case "aws":
		return awsVerb(tokens)
	case "az":
		return azVerb(tokens)
	case "gcloud":
		return gcloudVerb(tokens)
	}
	return mutate("", "unknown cloud provider "+provider+" (use aws, az or gcloud)")
}

// CloudAccount returns the account selector named on the command line: an AWS profile, an Azure
// subscription or a GCP project.
func CloudAccount(provider string, tokens []string) string {
	switch provider {
	case "aws":
		return flagValue(tokens, "--profile")
	case "az":
		return flagValue(tokens, "--subscription", "-s")
	case "gcloud":
		return flagValue(tokens, "--project")
	}
	return ""
}

// leadingWords returns the first n words of a cloud CLI line, skipping the flags the CLI is known to
// take: a flag in valueFlags with its value, a flag in boolFlags or one that carries its value
// (--flag=value) alone. ok is false when another flag comes before the nth word: the token after it
// may be its value, so the words after it cannot be told from flag values.
func leadingWords(tokens []string, n int, valueFlags, boolFlags []string) (words []string, ok bool) {
	for i := 0; i < len(tokens) && len(words) < n; i++ {
		switch t := tokens[i]; {
		case !looksLikeFlag(t):
			words = append(words, t)
		case strings.Contains(t, "="), in(t, boolFlags...):
		case in(t, valueFlags...):
			i++
		default:
			return words, false
		}
	}
	return words, true
}

// commandPath returns the words that name what an az or gcloud line does: its words from the start
// up to its first flag, after any leading flags the CLI is known to take (skipped as leadingWords
// skips them). A word after the first flag is that flag's value or an operand, never part of the
// path. ok is false when a leading flag is not a known one.
func commandPath(tokens, valueFlags, boolFlags []string) (path []string, ok bool) {
	i := 0
	for ; i < len(tokens) && looksLikeFlag(tokens[i]); i++ {
		switch t := tokens[i]; {
		case strings.Contains(t, "="), in(t, boolFlags...):
		case in(t, valueFlags...):
			i++
		default:
			return nil, false
		}
	}
	for ; i < len(tokens) && !looksLikeFlag(tokens[i]); i++ {
		path = append(path, tokens[i])
	}
	return path, true
}

// awsActingVerbs are aws operations that carry a read prefix and still act: this Cognito call sends
// a verification message.
var awsActingVerbs = []string{"get-user-attribute-verification-code"}

// awsTestReads are the aws test-* operations known to evaluate their input and change nothing. Any
// other test-* operation fails closed: an API Gateway test invocation runs the integration, an
// ElastiCache test failover fails the node over, a CodeCommit trigger test sends the events.
var awsTestReads = []string{"test-event-pattern", "test-render-template", "test-dns-answer", "test-metric-filter"}

// awsVerb classifies an aws line by its service and verb: its first two words, wherever the global
// flags stand. A line with no word at all prints the usage text or the version. A line with one
// word names no read-only verb, so it fails closed: `aws configure` writes the credentials file from
// its standard input (a pipe or a redirect in a shell line gives it one), `aws login` and `aws
// logout` change the local credentials, and a bare service name is at best a usage error. `aws help`
// is the one single word that only reads. An unknown flag before the verb fails closed too.
func awsVerb(tokens []string) Result {
	pos, ok := leadingWords(tokens, 2, awsValueFlags, awsBoolFlags)
	switch {
	case !ok:
		return mutate(strings.Join(pos, " "), "aws: an option before the verb is not one this classifier knows, "+
			"so the verb cannot be told from its value")
	case len(pos) == 0:
		return read("")
	case len(pos) == 1 && pos[0] == "help":
		return read("help")
	case len(pos) == 1:
		return mutate(pos[0], "aws "+pos[0]+" names no read-only verb (aws configure, login and logout change "+
			"local credentials)")
	}
	service, verb := pos[0], pos[1]
	if in(verb, cloudFileWriters...) {
		return mutate(verb, "aws "+service+" "+verb+" writes a file")
	}
	if service == "configure" {
		if in(verb, "list", "get", "list-profiles") {
			return read("configure " + verb)
		}
		return mutate("configure "+verb, "aws configure "+verb+" changes local credentials or settings")
	}
	if in(verb, awsActingVerbs...) || (strings.HasPrefix(verb, "test-") && !in(verb, awsTestReads...)) {
		return mutate(verb, "aws "+service+" "+verb+" acts although its name reads like a check")
	}
	if hasPrefixIn(verb, cloudReadPrefixes...) || (service == "s3" && in(verb, "ls", "presign")) {
		return read(verb)
	}
	return mutate(verb, "aws "+service+" "+verb+" changes cloud resources")
}

// azMutateWords are az verbs that change something. A command path that carries one of them, as a
// word or as the head of a hyphenated verb (delete-batch), is never a read, whatever word follows it.
var azMutateWords = []string{"create", "delete", "update", "set", "unset", "add", "remove", "purge", "start",
	"stop", "restart", "deallocate", "reset", "revoke", "assign", "unassign", "import", "export", "move", "invoke",
	"run", "deploy", "login", "logout", "clear", "rotate", "regenerate", "renew", "enable", "disable", "cancel",
	"approve", "reject", "upgrade", "install", "uninstall", "apply", "failover", "swap", "sync", "upload", "copy",
	"attach", "detach", "scale", "resize", "restore", "redeploy", "reimage", "generalize", "capture", "lock", "unlock"}

func azMutates(word string) bool {
	for _, w := range azMutateWords {
		if word == w || strings.HasPrefix(word, w+"-") {
			return true
		}
	}
	return false
}

// azVerb classifies an az line by its command path: group, subgroup and verb, the words before its
// first flag. The verb is the last of them, and it is a read only when it carries a read prefix and
// no word of the path is a mutating verb. A word after the first flag never decides: az takes far
// more value flags than this classifier knows, and the value of one of them (a repository, a
// pattern, a resource named after a read verb) is not the verb.
func azVerb(tokens []string) Result {
	path, ok := commandPath(tokens, azValueFlags, azBoolFlags)
	all := strings.Join(path, " ")
	switch {
	case !ok:
		return mutate("", "az: an option before the command is not one this classifier knows, so the command "+
			"cannot be told from its value")
	case len(path) == 0:
		return read("")
	}
	verb := path[len(path)-1]
	for _, word := range path {
		if in(word, cloudFileWriters...) {
			return mutate(word, "az "+all+" writes a file (get-credentials writes the kubeconfig)")
		}
		if azMutates(word) {
			return mutate(word, "az "+all+" changes cloud resources or local state")
		}
	}
	if hasPrefixIn(verb, cloudReadPrefixes...) {
		return read(verb)
	}
	return mutate(verb, "az "+all+" changes cloud resources or local state")
}

var gcloudMutatePrefixes = []string{"create", "delete", "update", "add-", "remove-", "set-", "enable", "disable",
	"deploy", "run", "ssh", "scp", "start", "stop", "reset", "resize", "suspend", "resume", "attach-", "detach-",
	"move", "import", "export", "activate", "revoke", "login", "init", "patch", "submit", "cancel", "restart",
	"apply", "rollback", "promote", "migrate", "upgrade", "install", "uninstall", "push", "pull", "publish",
	"invoke", "execute", "insert", "replace", "undelete", "purge", "clear", "kill", "terminate", "unset", "set",
	"simulate-maintenance-event"}

// gcloudGroups are command groups spelled like a mutating verb: gcloud run and gcloud deploy are
// products when they come first, so the verb is found among the tokens after them.
var gcloudGroups = map[string]bool{"run": true, "deploy": true}

// gcloudReadVerbs are gcloud reads that are no cloudReadPrefixes: logging read.
var gcloudReadVerbs = []string{"read"}

// gcloudVerb classifies a gcloud line by its command path: the words before its first flag, after a
// leading product group. A mutating verb anywhere in the path decides first, so neither a group named
// like a read (firebase test android run) nor a resource named after a read verb (delete describe-x)
// can turn a mutation into a read; then the first read verb; a path with neither fails closed. A word
// after the first flag never decides: it is a flag's value or an operand.
func gcloudVerb(tokens []string) Result {
	all, ok := commandPath(tokens, gcloudValueFlags, gcloudBoolFlags)
	if !ok {
		return mutate("", "gcloud: an option before the command is not one this classifier knows, so the "+
			"command cannot be told from its value")
	}
	pos := all
	if len(pos) > 0 && gcloudGroups[pos[0]] {
		pos = pos[1:]
	}
	for _, p := range pos {
		if in(p, cloudFileWriters...) {
			return mutate(p, "gcloud "+strings.Join(all, " ")+" writes a file (get-credentials writes the kubeconfig)")
		}
		if hasPrefixIn(p, gcloudMutatePrefixes...) {
			return mutate(p, "gcloud "+strings.Join(all, " ")+" changes cloud resources or local state")
		}
	}
	for _, p := range pos {
		if hasPrefixIn(p, cloudReadPrefixes...) || in(p, gcloudReadVerbs...) {
			return read(p)
		}
	}
	verb := ""
	if len(all) > 0 {
		verb = all[len(all)-1]
	}
	return mutate(verb, "gcloud "+strings.Join(all, " ")+" has no read-only verb (describe, list, get-*, ...)")
}
