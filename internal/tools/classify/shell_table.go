package classify

// readOnlyPrograms are commands whose every invocation only reads. Programs with read and write
// forms (find, sed, awk, curl, make, git, kubectl, ...) are handled in shellProgram.
var readOnlyPrograms = map[string]bool{
	"cat": true, "ls": true, "ll": true, "dir": true, "grep": true, "egrep": true, "fgrep": true, "rg": true, "ag": true,
	"ack": true, "head": true, "tail": true, "wc": true, "sort": true, "uniq": true, "cut": true, "tr": true, "jq": true,
	"yq": true, "diff": true, "cmp": true, "comm": true, "echo": true, "printf": true, "date": true, "cal": true,
	"uname": true, "hostname": true, "whoami": true, "id": true, "groups": true, "env": true, "printenv": true,
	"pwd": true, "which": true, "whereis": true, "type": true, "file": true, "stat": true, "du": true, "df": true,
	"free": true, "vmstat": true, "iostat": true, "ps": true, "pgrep": true, "pstree": true, "tree": true,
	"realpath": true, "basename": true, "dirname": true, "readlink": true, "test": true, "true": true, "false": true,
	"seq": true, "expr": true, "bc": true, "column": true, "paste": true, "nl": true, "tac": true, "rev": true,
	"fold": true, "fmt": true, "less": true, "more": true, "base64": true, "md5sum": true, "md5": true,
	"sha1sum": true, "sha256sum": true, "shasum": true, "cksum": true, "xxd": true, "hexdump": true, "od": true,
	"strings": true, "sleep": true, "tput": true, "locale": true, "getent": true, "ldd": true, "otool": true,
	"nproc": true, "arch": true, "ulimit": true, "zcat": true, "envsubst": true, "look": true, "join": true,
	"journalctl": true, "dmesg": true, "last": true, "w": true, "who": true, "uptime": true, "ss": true,
	"netstat": true, "lsof": true, "ifconfig": true, "dig": true, "nslookup": true, "host": true, "ping": true,
	"traceroute": true, "tracepath": true, "mtr": true, "apt-cache": true, "dpkg-query": true, "xmllint": true,
	"apt-mark": false,
}

// subcommandReads lists, for programs whose first argument selects the operation, the operations
// that only read.
var subcommandReads = map[string][]string{
	"systemctl": {"status", "list-units", "list-unit-files", "list-timers", "list-dependencies", "is-active",
		"is-enabled", "is-failed", "show", "cat"},
	"brew":      {"list", "ls", "info", "deps", "outdated", "doctor", "config", "search", "--version", "-v"},
	"apt":       {"list", "show", "search", "policy", "depends", "rdepends"},
	"apt-get":   {"changelog", "download"},
	"yum":       {"list", "info", "search", "repolist", "check-update", "provides", "deplist"},
	"dnf":       {"list", "info", "search", "repolist", "check-update", "provides", "deplist"},
	"pip":       {"list", "show", "freeze", "check", "index", "--version", "-V"},
	"pip3":      {"list", "show", "freeze", "check", "index", "--version", "-V"},
	"npm":       {"ls", "list", "view", "info", "outdated", "why", "explain", "-v", "--version", "config"},
	"pnpm":      {"ls", "list", "view", "info", "outdated", "why", "-v", "--version"},
	"yarn":      {"list", "info", "why", "--version", "-v"},
	"go":        {"version", "env", "list", "doc"},
	"cargo":     {"--version", "metadata", "tree"},
	"ip":        {"addr", "address", "a", "route", "r", "link", "l", "neigh", "n", "rule", "-V"},
	"openssl":   {"x509", "s_client", "verify", "dgst", "version", "req"},
	"gh":        {"auth", "pr", "issue", "repo", "run", "release", "workflow", "search", "browse", "--version"},
	"dpkg":      {"-l", "-s", "-L", "-S", "--list", "--status"},
	"rpm":       {"-q", "-qa", "-qi", "-ql", "-qf", "--version"},
	"tar":       {"-t", "-tf", "-tvf", "-tzf", "-tjf", "t", "tf", "tvf", "tzf", "-tzvf", "--list"},
	"unzip":     {"-l", "-t", "-Z"},
	"launchctl": {"list", "print", "version"},
	"crontab":   {"-l"},
}
