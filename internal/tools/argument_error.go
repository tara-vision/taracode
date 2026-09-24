package tools

import "errors"

// argumentChecks are the built-in tools that refuse some arguments before anything runs, each with the
// check its own Run applies. Only kubectl for now: its argument errors used to reach the policy as an
// invocation with no verb and no targets (ruling P3-R59).
var argumentChecks = map[string]func(args map[string]any) error{
	"kubectl": func(args map[string]any) error {
		_, err := KubectlArgv(args)
		return err
	},
}

// ArgumentError is the error the named built-in tool refuses a call's arguments with before anything
// runs (a kubectl namespace given twice with different values, args that start with another verb), or
// nil when the tool takes them. The gate refuses such a call at the classify step with rule classifier
// and this reason, in every mode: the mode rule's advice to switch modes would be wrong, and the policy
// cannot read the targets of a command that will never run. The error is redacted as a tool's is.
func (r *Registry) ArgumentError(name string, args map[string]any) error {
	check, ok := argumentChecks[name]
	if !ok || r.IsMCP(name) {
		return nil
	}
	if _, registered := r.Get(name); !registered {
		return nil
	}
	if err := check(args); err != nil {
		return errors.New(r.redact(err.Error()))
	}
	return nil
}
