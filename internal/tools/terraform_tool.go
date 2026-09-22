package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools/classify"
	"github.com/tara-vision/taracode/internal/tools/shellwords"
	"github.com/tara-vision/taracode/internal/tools/tfplan"
)

const terraformTimeout = 600 * time.Second

// planRecord is a plan produced in this session, keyed by the absolute directory.
type planRecord struct {
	file    string
	summary string
	at      time.Time
}

type terraformState struct {
	mu    sync.Mutex
	plans map[string]planRecord
}

// noColorCommands accept -no-color.
var noColorCommands = map[string]bool{
	"plan": true, "apply": true, "destroy": true, "init": true, "validate": true,
	"show": true, "output": true, "refresh": true, "import": true,
}

// TerraformTool runs terraform in a directory. plan always writes a session plan file and returns
// its summary; apply applies that file and refuses without one.
func TerraformTool() *Tool {
	st := &terraformState{plans: map[string]planRecord{}}
	return &Tool{
		Name: "terraform", ReadForm: true,
		Description: "Run terraform in a directory. plan returns a summary and keeps the plan for apply; apply applies " +
			"the plan from this session. Read commands: validate, plan, show, state list, output, graph, fmt -check, " +
			"init -backend=false.",
		Params: []Param{
			{Name: "command", Type: "string",
				Description: "terraform command, for example plan, apply, validate, state, output", Required: true},
			{Name: "dir", Type: "string", Description: "Directory with the configuration (default: working directory)"},
			{Name: "args", Type: "string",
				Description: "Extra arguments, for example \"-var-file=prod.tfvars\" or \"list\" for state"},
		},
		Classify: func(args map[string]any, workingDir string) policy.Invocation {
			command, raw := argString(args, "command"), argString(args, "args")
			words, err := shellwords.Words(raw)
			if err != nil {
				return policy.Invocation{Tool: "terraform", Classification: policy.Mutate, Reason: err.Error(),
					Command: strings.TrimSpace("terraform " + command + " " + raw)}
			}
			res := classify.Terraform(command, words)
			return policy.Invocation{Tool: "terraform", Verb: res.Verb, Classification: res.Classification, Reason: res.Reason,
				Command: strings.TrimSpace("terraform " + command + " " + raw),
				Targets: policy.Targets{Paths: []string{resolvePath(argString(args, "dir"), workingDir)}}}
		},
		Run: func(ctx context.Context, args map[string]any, workingDir string) (string, error) {
			command, err := required(args, "command")
			if err != nil {
				return "", err
			}
			words, err := shellwords.Words(argString(args, "args"))
			if err != nil {
				return "", err
			}
			dir := resolvePath(argString(args, "dir"), workingDir)
			ctx, cancel := withTimeout(ctx, terraformTimeout)
			defer cancel()
			switch command {
			case "plan":
				return st.plan(ctx, dir, words)
			case "apply":
				return st.apply(ctx, dir, words)
			}
			argv := append([]string{command}, words...)
			if noColorCommands[command] && !hasWord(words, "-no-color") {
				argv = append(argv, "-no-color")
			}
			return runCommand(ctx, dir, "terraform", argv...)
		},
		DryRun: func(_ context.Context, args map[string]any, workingDir string) (string, error) {
			if argString(args, "command") != "apply" {
				return "", ErrNoDryRun
			}
			rec, ok := st.get(resolvePath(argString(args, "dir"), workingDir))
			if !ok {
				return "", fmt.Errorf("no plan from this session for this directory; run terraform plan first")
			}
			return fmt.Sprintf("Plan from %s:\n%s", rec.at.Format(time.Kitchen), rec.summary), nil
		},
	}
}

func (st *terraformState) get(dir string) (planRecord, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	rec, ok := st.plans[dir]
	return rec, ok
}

func (st *terraformState) plan(ctx context.Context, dir string, words []string) (string, error) {
	f, err := os.CreateTemp("", "taracode-*.tfplan")
	if err != nil {
		return "", err
	}
	planFile := f.Name()
	_ = f.Close()
	argv := append([]string{"plan", "-input=false", "-no-color", "-out=" + planFile}, words...)
	if out, err := runCommand(ctx, dir, "terraform", argv...); err != nil {
		_ = os.Remove(planFile)
		return out, err
	}
	raw, err := runCommand(ctx, dir, "terraform", "show", "-json", planFile)
	if err != nil {
		return raw, err
	}
	summary, err := tfplan.Summarize([]byte(raw))
	if err != nil {
		return "", err
	}
	text := summary.String()
	st.mu.Lock()
	if old, ok := st.plans[dir]; ok {
		_ = os.Remove(old.file)
	}
	st.plans[dir] = planRecord{file: planFile, summary: text, at: time.Now()}
	st.mu.Unlock()
	return text + "\nThe plan is saved for terraform apply in this session.", nil
}

func (st *terraformState) apply(ctx context.Context, dir string, words []string) (string, error) {
	st.mu.Lock()
	rec, ok := st.plans[dir]
	delete(st.plans, dir)
	st.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("no plan from this session for %s; run terraform plan first", dir)
	}
	defer func() { _ = os.Remove(rec.file) }()
	argv := append([]string{"apply", "-input=false", "-no-color"}, words...)
	argv = append(argv, rec.file)
	return runCommand(ctx, dir, "terraform", argv...)
}
