package policy

// StarterYAML is the policy file /init writes. It parses to the same rules as Default().
const StarterYAML = `# taracode policy (see: taracode doctor, /policy show).
# The model never sees this file. .taracode/policy.yaml merges over ~/.taracode/policy.yaml:
# lists are unioned and booleans take the stricter value. With no policy file at all, taracode
# uses a built-in policy identical to this one. Patterns are globs (* ? and ** in paths).
version: 1
mode: investigate               # the mode a session starts in: investigate or operate
protected:                      # never mutated in operate mode (hard deny, printed reason)
  kube_contexts: ["*prod*", "*production*"]
  kube_namespaces: ["kube-system"]
  cloud_accounts: []            # AWS account ids, Azure subscription ids, GCP project ids, or *globs*
  paths: ["**/*.tfstate", ".git/**"]
  hosts: []
deny:                           # refused outright; the last pattern keeps the policy files safe
  commands: ["rm -rf /*", "kubectl delete namespace *", "terraform destroy*", "*.taracode/policy.yaml*"]
require_dry_run:                # shown before the permission prompt
  kubectl_apply: true           # kubectl diff first
  terraform_apply: true         # a plan from this session, its summary first
  helm_upgrade: true            # helm --dry-run first (upgrade and install)
redact:
  enabled: true                 # secrets in tool output become [redacted:<kind>]
  extra_patterns: []            # additional Go regular expressions
mcp:
  trust_read_only_hint: true    # a server's readOnlyHint gives its tool a read form (investigate mode)
  read_only: {}                 # per server, the tools (globs) that count as reads when the hint is not trusted
`
