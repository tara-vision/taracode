// Package tfplan turns terraform's JSON plan output into the short summary the model sees instead
// of the raw plan: counts, the changed addresses with their action, and a risk list.
package tfplan

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Change is one resource change.
type Change struct {
	Address string
	Type    string
	Action  string // create | update | delete | replace | read
}

// Summary is what the model and the user see.
type Summary struct {
	TerraformVersion string
	Adds             int
	Changes          int
	Destroys         int
	Replaces         int
	Resources        []Change
	Risks            []string
	Outputs          int
}

type planJSON struct {
	TerraformVersion string `json:"terraform_version"`
	ResourceChanges  []struct {
		Address string `json:"address"`
		Type    string `json:"type"`
		Change  struct {
			Actions []string `json:"actions"`
		} `json:"change"`
	} `json:"resource_changes"`
	OutputChanges map[string]struct {
		Actions []string `json:"actions"`
	} `json:"output_changes"`
}

var (
	statefulType = regexp.MustCompile(`(?i)db_instance|rds_cluster|sql_database|sql_server|dynamodb_table|` +
		`s3_bucket$|storage_bucket|storage_account|efs_file_system|ebs_volume|_disk$|_volume$|` +
		`persistent_volume|cosmosdb|elasticache|redis|kms_key|secret`)
	accessType = regexp.MustCompile(`(?i)_iam_|_role$|_policy|security_group|network_security|firewall|_acl`)
)

// Summarize parses `terraform show -json` output.
func Summarize(planData []byte) (Summary, error) {
	var p planJSON
	if err := json.Unmarshal(planData, &p); err != nil {
		return Summary{}, fmt.Errorf("parse plan JSON: %w", err)
	}
	s := Summary{TerraformVersion: p.TerraformVersion}
	for _, rc := range p.ResourceChanges {
		action := actionOf(rc.Change.Actions)
		if action == "no-op" {
			continue
		}
		s.Resources = append(s.Resources, Change{Address: rc.Address, Type: rc.Type, Action: action})
		switch action {
		case "create":
			s.Adds++
		case "update":
			s.Changes++
		case "delete":
			s.Destroys++
		case "replace":
			s.Replaces++
		}
		s.Risks = append(s.Risks, risks(rc.Address, rc.Type, action)...)
	}
	for _, oc := range p.OutputChanges {
		if actionOf(oc.Actions) != "no-op" {
			s.Outputs++
		}
	}
	return s, nil
}

func actionOf(actions []string) string {
	switch strings.Join(actions, ",") {
	case "create":
		return "create"
	case "update":
		return "update"
	case "delete":
		return "delete"
	case "delete,create", "create,delete":
		return "replace"
	case "read":
		return "read"
	}
	return "no-op"
}

func risks(address, typ, action string) []string {
	var out []string
	destructive := action == "delete" || action == "replace"
	if destructive && statefulType.MatchString(typ) {
		out = append(out, fmt.Sprintf("%s: %s of a stateful resource (data loss)", address, action))
	}
	if accessType.MatchString(typ) {
		out = append(out, fmt.Sprintf("%s: %s changes access control or firewall rules", address, action))
	}
	if destructive && strings.Contains(strings.ToLower(address), "prod") {
		out = append(out, fmt.Sprintf("%s: %s of a production-named resource", address, action))
	}
	return out
}

// String renders the summary the way terraform prints its own plan line, then the changes and risks.
func (s Summary) String() string {
	if len(s.Resources) == 0 && s.Outputs == 0 {
		return "No changes. Your infrastructure matches the configuration."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Plan: %d to add, %d to change, %d to destroy, %d to replace",
		s.Adds, s.Changes, s.Destroys, s.Replaces)
	if s.Outputs > 0 {
		fmt.Fprintf(&b, ", %d output(s) changed", s.Outputs)
	}
	b.WriteString(".\n")
	marks := map[string]string{"create": "+", "update": "~", "delete": "-", "replace": "-/+", "read": "<="}
	for _, c := range s.Resources {
		fmt.Fprintf(&b, "  %-3s %s (%s)\n", marks[c.Action], c.Address, c.Action)
	}
	if len(s.Risks) > 0 {
		b.WriteString("Risks:\n")
		for _, r := range s.Risks {
			fmt.Fprintf(&b, "  - %s\n", r)
		}
	}
	return b.String()
}
