package classify

import "strings"

var cloudReadPrefixes = []string{"describe", "get", "list", "ls", "show", "search", "filter", "lookup", "query",
	"scan", "head", "batch-get", "estimate", "simulate", "test", "validate", "check", "wait", "tail", "preview",
	"select", "exists", "count", "print", "version", "help", "info", "whoami"}

var awsValueFlags = []string{"--profile", "--region", "--output", "--query", "--endpoint-url", "--cli-connect-timeout"}
var azValueFlags = []string{"--subscription", "-s", "-n", "--name", "-g", "--resource-group", "-o", "--output",
	"--query", "-l", "--location"}
var gcloudValueFlags = []string{"--project", "--zone", "--region", "--format", "--filter", "--account",
	"--configuration"}

// cloudFileWriters are the verbs that look like reads but write a file: get-credentials merges a
// cluster into the kubeconfig (az, gcloud), and the AWS operations that stream their response
// into the outfile operand they require.
var cloudFileWriters = []string{"get-credentials", "get-object", "get-object-torrent", "get-job-output", "get-export",
	"get-sdk", "get-media", "get-clip", "get-snapshot-block", "get-thing-shadow", "get-raw-message-content",
	"get-package-version-asset"}

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

func awsVerb(tokens []string) Result {
	pos := positionals(tokens, awsValueFlags...)
	if len(pos) < 2 {
		return read(strings.Join(pos, " "))
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
	if hasPrefixIn(verb, cloudReadPrefixes...) || (service == "s3" && in(verb, "ls", "presign")) {
		return read(verb)
	}
	return mutate(verb, "aws "+service+" "+verb+" changes cloud resources")
}

func azVerb(tokens []string) Result {
	pos := positionals(tokens, azValueFlags...)
	if len(pos) == 0 {
		return read("")
	}
	verb := pos[len(pos)-1]
	if in(verb, cloudFileWriters...) {
		return mutate(verb, "az "+strings.Join(pos, " ")+" writes a file (get-credentials writes the kubeconfig)")
	}
	if hasPrefixIn(verb, cloudReadPrefixes...) {
		return read(verb)
	}
	return mutate(verb, "az "+strings.Join(pos, " ")+" changes cloud resources or local state")
}

var gcloudMutatePrefixes = []string{"create", "delete", "update", "add-", "remove-", "set-", "enable", "disable",
	"deploy", "run", "ssh", "scp", "start", "stop", "reset", "resize", "suspend", "resume", "attach-", "detach-",
	"move", "import", "export", "activate", "revoke", "login", "init", "patch", "submit", "cancel", "restart",
	"apply", "rollback", "promote", "migrate", "upgrade", "install", "uninstall", "push", "pull", "publish",
	"invoke", "execute", "insert", "replace", "undelete", "purge", "clear", "kill", "terminate", "unset", "set"}

// gcloudVerb scans the positional tokens in order: the first token that is a known verb decides, so
// a resource named after a read verb ("delete describe-x") cannot turn a mutation into a read.
func gcloudVerb(tokens []string) Result {
	pos := positionals(tokens, gcloudValueFlags...)
	for _, p := range pos {
		if in(p, cloudFileWriters...) {
			return mutate(p, "gcloud "+strings.Join(pos, " ")+" writes a file (get-credentials writes the kubeconfig)")
		}
		if hasPrefixIn(p, gcloudMutatePrefixes...) {
			return mutate(p, "gcloud "+strings.Join(pos, " ")+" changes cloud resources or local state")
		}
		if hasPrefixIn(p, cloudReadPrefixes...) {
			return read(p)
		}
	}
	verb := ""
	if len(pos) > 0 {
		verb = pos[len(pos)-1]
	}
	return mutate(verb, "gcloud "+strings.Join(pos, " ")+" has no read-only verb (describe, list, get-*, ...)")
}
