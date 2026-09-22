package tfplan

import (
	"os"
	"strings"
	"testing"
)

func TestSummarizeCountsActionsAndRisks(t *testing.T) {
	data, err := os.ReadFile("testdata/plan.json")
	if err != nil {
		t.Fatal(err)
	}
	s, err := Summarize(data)
	if err != nil {
		t.Fatal(err)
	}
	if s.Adds != 1 || s.Changes != 2 || s.Destroys != 1 || s.Replaces != 1 || s.Outputs != 1 || s.TerraformVersion != "1.9.5" {
		t.Fatalf("counts %+v", s)
	}
	if len(s.Resources) != 6 {
		t.Fatalf("resources %+v", s.Resources)
	}
	if s.Resources[2].Action != "replace" || s.Resources[5].Action != "read" {
		t.Errorf("actions %+v", s.Resources)
	}
	text := s.String()
	for _, want := range []string{"1 to add", "2 to change", "1 to destroy", "1 to replace", "aws_db_instance.prod", "stateful", "aws_iam_role.deploy", "access control", "aws_security_group.edge", "production"} {
		if !strings.Contains(text, want) {
			t.Errorf("summary lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "aws_vpc.main") {
		t.Error("no-op resources must not be listed")
	}
}

func TestSummarizeEmptyAndInvalid(t *testing.T) {
	s, err := Summarize([]byte(`{"format_version":"1.2","terraform_version":"1.9.5","resource_changes":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.String(), "No changes") {
		t.Errorf("empty plan: %q", s.String())
	}
	if _, err := Summarize([]byte(`{not json`)); err == nil {
		t.Error("invalid JSON must error")
	}
}
