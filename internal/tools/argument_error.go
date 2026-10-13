package tools

import (
	"errors"
	"fmt"
	"strings"
)

// argumentChecks are the built-in tools that refuse some arguments before anything runs, each with the
// check its own Run applies: kubectl, whose argument errors used to reach the policy as an invocation with
// no verb and no targets (ruling P3-R59), and terraform, whose command must be a command and not an option
// (-chdir=infra apply would run as a plain call, past the plan the tool promises before an apply).
var argumentChecks = map[string]func(args map[string]any) error{
	"kubectl": func(args map[string]any) error {
		_, err := KubectlArgv(args)
		return err
	},
	"terraform": func(args map[string]any) error {
		command, _, err := TerraformCommand(args)
		if err == nil && strings.HasPrefix(command, "-") {
			return fmt.Errorf("command %q is an option: name the terraform command and give the directory as dir", command)
		}
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
