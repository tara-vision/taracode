package classify

import "strings"

// writeFlags are the options that make a read-listed program write a file, run a program or change
// system state. long names match written in full or as --name=value, and with gnu also as any
// abbreviation GNU getopt_long accepts. short letters match alone or in a cluster (-ro), up to the
// first letter in valued, whose value takes the rest of the token (-ojson).
type writeFlags struct {
	long   []string
	gnu    bool
	short  string
	valued string
	reason string
}

// readProgramFlags lists the read-listed programs that have such options.
var readProgramFlags = map[string]writeFlags{
	"rg": {long: []string{"--pre"}, reason: "rg --pre runs a program on every file it searches"},
	"sort": {long: []string{"--output", "--compress-program"}, gnu: true, short: "o", valued: "kSTt",
		reason: "sort -o writes a file and --compress-program runs a program"},
	"yq": {long: []string{"--inplace", "--in-place", "--split-exp"}, gnu: true, short: "is", valued: "opIf",
		reason: "yq -i rewrites the file and -s writes a file per document"},
	"tree": {short: "oR", reason: "tree -o and -R write files"},
	"less": {long: []string{"--log-file", "--LOG-FILE"}, gnu: true, short: "oO", valued: "bhjkpPtTxyz#",
		reason: "less -o and -O copy the input to a file"},
	"base64": {long: []string{"--output"}, gnu: true, short: "o", valued: "bwi", reason: "base64 -o writes a file"},
	"xmllint": {long: []string{"-o", "-output", "--output", "-shell", "--shell"},
		reason: "xmllint --output writes a file and --shell can write files"},
	"file": {long: []string{"--compile"}, gnu: true, short: "C", valued: "efFmP",
		reason: "file -C writes a compiled magic file"},
	"ack": {long: []string{"--pager", "--output", "--ackrc"}, gnu: true,
		reason: "ack --pager and --output run programs, --ackrc can set them"},
	"journalctl": {long: []string{"--vacuum-size", "--vacuum-time", "--vacuum-files", "--rotate", "--flush", "--sync",
		"--relinquish-var", "--smart-relinquish-var", "--update-catalog", "--setup-keys"}, gnu: true,
		reason: "journalctl --vacuum-*, --rotate, --flush and --setup-keys change the journal"},
	"dmesg": {long: []string{"--clear", "--read-clear", "--console-level", "--console-off", "--console-on"}, gnu: true,
		short: "cCDEn", valued: "lfsF", reason: "dmesg -c, -C, -n, -D and -E change the kernel log"},
	"ss": {long: []string{"--kill"}, gnu: true, short: "K", valued: "fAFN", reason: "ss -K closes sockets"},
	"printf": {short: "v",
		reason: "printf -v sets a shell variable, which a later command can expand into options"},
}

// readProgramWrites catches the write forms of the read-listed programs: the options in
// readProgramFlags, and the operands that make uniq and xxd write a file, arch run a program,
// hostname rename the host and ifconfig configure an interface. ok is false when the read stands.
func readProgramWrites(prog string, rest []string) (Result, bool) {
	if f, ok := readProgramFlags[prog]; ok {
		matched := shortFlag(rest, f.short, f.valued)
		if f.gnu {
			matched = matched || hasGNUFlag(rest, nil, f.long...)
		} else {
			matched = matched || hasFlag(rest, f.long...)
		}
		if matched {
			return mutate(prog, f.reason), true
		}
	}
	switch prog {
	case "uniq":
		if len(operands(rest, "-f", "-s", "-w", "--skip-fields", "--skip-chars", "--check-chars")) > 1 {
			return mutate(prog, "uniq writes its second operand"), true
		}
	case "xxd":
		if len(operands(rest, "-c", "-cols", "-g", "-groupsize", "-l", "-len", "-s", "-seek", "-o", "-offset",
			"-n", "-name", "-R")) > 1 {
			return mutate(prog, "xxd writes its second operand"), true
		}
	case "arch":
		if len(rest) > 1 || len(rest) == 1 && !in(rest[0], "-h", "--help", "--version") {
			return mutate(prog, "arch runs the program it is given"), true
		}
	case "hostname":
		if len(operands(rest)) > 0 || hasFlag(rest, "-F", "--file", "-b", "--boot") {
			return mutate(prog, "hostname with a name or a file sets the host name"), true
		}
	case "ifconfig":
		if len(operands(rest)) > 1 {
			return mutate(prog, "ifconfig with more than an interface name configures it"), true
		}
	}
	return Result{}, false
}

// ipReadCommands are the ip commands that only display.
var ipReadCommands = []string{"show", "list", "ls", "lst", "get", "help"}

// opensslWriteFlags write a file or load code: output files, the random seed, the TLS session and
// key logs, a signing CA's serial file, key generation (which writes privkey.pem when -keyout is
// missing), engines and providers.
var opensslWriteFlags = []string{"-out", "-keyout", "-writerand", "-sess_out", "-msgfile", "-keylogfile",
	"-CAcreateserial", "-CAserial", "-CA", "-new", "-newkey", "-x509", "-engine", "-provider", "-provider-path"}

// tarWriteLong are the GNU tar options that run a program or write a file even when listing;
// tarOwnLong are tar's other options that are prefixes of one of them (--checkpoint only prints).
var (
	tarWriteLong = []string{"--use-compress-program", "--checkpoint-action", "--info-script", "--new-volume-script",
		"--to-command", "--rsh-command", "--rmt-command", "--index-file", "--volno-file"}
	tarOwnLong = []string{"--checkpoint"}
)

// subcommandWrites catches the write forms of the subcommand-table reads: ip's configuring commands
// (the object is a read, set, add and flush are not), openssl's output files and code loading, npm
// config and go env writes, go list running a tool, and tar's program-running options. ok is false
// when the read stands.
func subcommandWrites(prog string, rest []string) (Result, bool) {
	sub, args := rest[0], rest[1:]
	switch prog {
	case "ip":
		if cmd := first(args); sub != "-V" && cmd != "" && !in(cmd, ipReadCommands...) {
			return mutate("ip "+sub+" "+cmd, "ip "+sub+" "+cmd+" changes the network configuration"), true
		}
	case "openssl":
		return opensslWrites(sub, args)
	case "npm", "go":
		return settingsWrites(prog, sub, args)
	case "tar":
		if hasGNUFlag(rest, tarOwnLong, tarWriteLong...) || shortFlag(rest, "IF", "fCTXbKLNVHg") {
			return mutate("tar", "tar -I, -F and --to-command run programs"), true
		}
	}
	return Result{}, false
}

// opensslWrites finds an option in opensslWriteFlags, written with one dash or two.
func opensslWrites(sub string, args []string) (Result, bool) {
	for _, t := range args {
		name, _, _ := strings.Cut(strings.TrimLeft(t, "-"), "=")
		if strings.HasPrefix(t, "-") && in("-"+name, opensslWriteFlags...) {
			return mutate("openssl "+sub, "openssl "+sub+" "+t+" writes a file or loads code"), true
		}
	}
	return Result{}, false
}

// settingsWrites catches npm config and go env writes, and go list running a tool or rewriting
// go.mod.
func settingsWrites(prog, sub string, args []string) (Result, bool) {
	switch {
	case prog == "npm" && sub == "config" && !in(first(args), "", "get", "list", "ls"):
		return mutate("npm config "+first(args), "npm config "+first(args)+" changes the npm configuration"), true
	case prog == "go" && sub == "env" && hasFlag(args, "-w", "-u"):
		return mutate("go env", "go env -w and -u change the Go environment file"), true
	case prog == "go" && sub == "list" &&
		(goFlagSet(args, "toolexec") || goFlagSet(args, "exec") || flagValue(args, "-mod") == "mod"):
		return mutate("go list", "go list -toolexec and -exec run programs, -mod=mod rewrites go.mod"), true
	}
	return Result{}, false
}

// curlWriteLong are the curl options that write a file, send a body or a command, or read a config
// file that can do either. curl 7 accepts any unambiguous abbreviation of a long option;
// curlOwnLong are curl's other options that are prefixes of one of them (--cookie sends cookies,
// --cookie-jar writes them).
var (
	curlWriteLong = []string{"--output", "--remote-name", "--remote-name-all", "--output-dir", "--upload-file",
		"--data", "--data-raw", "--data-binary", "--data-urlencode", "--data-ascii", "--form", "--form-string",
		"--json", "--cookie-jar", "--dump-header", "--trace", "--trace-ascii", "--stderr", "--libcurl", "--etag-save",
		"--config", "--hsts", "--alt-svc", "--quote"}
	curlOwnLong = []string{"--cookie"}
)

const (
	curlWriteShort  = "oOTdFcDKQ"
	curlValuedShort = "AbcCdDeEFHKmoQrTuUwxXyYz"
)

func curlResult(rest []string) Result {
	if hasGNUFlag(rest, curlOwnLong, curlWriteLong...) || shortFlag(rest, curlWriteShort, curlValuedShort) {
		return mutate("curl", "curl with an output file, a request body or a config file is not a plain GET")
	}
	method := shortValue(rest, 'X', curlValuedShort)
	if method == "" {
		method = gnuFlagValue(rest, "--request")
	}
	if method = strings.ToUpper(method); method != "" && method != "GET" && method != "HEAD" {
		return mutate("curl", "curl -X "+method+" is not a read")
	}
	writeOut := shortValue(rest, 'w', curlValuedShort) + gnuFlagValue(rest, "--write-out")
	if strings.Contains(writeOut, "%output{") {
		return mutate("curl", "curl -w %output{...} writes a file")
	}
	return read("curl")
}

// wgetWriteLong and wgetWriteShort are the wget options that write a file besides the download
// (logs, cookies, WARC, directories), send a body or a method, run .wgetrc commands or recurse.
var wgetWriteLong = []string{"--output-file", "--append-output", "--execute", "--background", "--post-data",
	"--post-file", "--method", "--body-data", "--body-file", "--config", "--save-cookies", "--warc-file",
	"--recursive", "--mirror", "--page-requisites", "--force-directories", "--convert-links"}

const (
	wgetWriteShort  = "oaebrmpxkN"
	wgetValuedShort = "eoaiBtOTwQlDARIXPUn"
)

// wgetResult: wget reads only when the download goes to standard output (-O-) and no other option
// writes, sends or recurses.
func wgetResult(rest []string) Result {
	toStdout := shortValue(rest, 'O', wgetValuedShort) == "-" || gnuFlagValue(rest, "--output-document") == "-"
	if !toStdout {
		return mutate("wget", "wget writes the download to a file (use curl or wget -O-)")
	}
	if hasGNUFlag(rest, nil, wgetWriteLong...) || shortFlag(rest, wgetWriteShort, wgetValuedShort) {
		return mutate("wget", "wget with a log, cookie or WARC file, a request body, -e commands or recursion "+
			"is not a plain download")
	}
	return read("wget")
}
