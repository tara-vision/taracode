package tools

import (
	"strings"
	"testing"
)

// TestCloudToolDescriptionTeachesTheCommandShapes pins the 3.1.1 description. The first scoreboard
// showed models inventing aws subcommands ("iam role list"), which the verb-position classifier
// reads as mutations and investigate mode refuses; the description now shows each CLI's shape with
// read examples and names the read verbs.
func TestCloudToolDescriptionTeachesTheCommandShapes(t *testing.T) {
	d := CloudTool().Description
	for _, want := range []string{
		"aws <service> <verb-noun>", "iam list-roles", "<group> [subgroup] <verb>", "compute instances list",
		"describe, get, list, ls and show read",
	} {
		if !strings.Contains(d, want) {
			t.Errorf("description missing %q:\n%s", want, d)
		}
	}
}
