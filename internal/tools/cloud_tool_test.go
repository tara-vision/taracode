package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

func TestCloudToolRunsTheProviderBinary(t *testing.T) {
	fakeBin(t, "aws", "")
	tool := CloudTool()
	out, err := tool.Run(context.Background(), map[string]any{"provider": "aws", "args": "sts get-caller-identity --profile dev"}, "")
	if err != nil || !strings.Contains(out, "aws sts get-caller-identity --profile dev") {
		t.Fatalf("%q %v", out, err)
	}
	inv := tool.Classify(map[string]any{"provider": "aws", "args": "ec2 terminate-instances --instance-ids i-1 --profile prod"}, "/w")
	if inv.Classification != policy.Mutate || inv.Targets.CloudAccount != "prod" || inv.Command != "aws ec2 terminate-instances --instance-ids i-1 --profile prod" {
		t.Errorf("%+v", inv)
	}
	if inv := tool.Classify(map[string]any{"provider": "gcloud", "args": "projects list"}, "/w"); inv.Classification != policy.Read {
		t.Errorf("%+v", inv)
	}
	if inv := tool.Classify(map[string]any{"provider": "doctl", "args": "x"}, "/w"); inv.Classification != policy.Mutate {
		t.Errorf("unknown providers are mutate: %+v", inv)
	}
	if _, err := tool.Run(context.Background(), map[string]any{"provider": "doctl", "args": "x"}, ""); err == nil {
		t.Error("unknown provider must error")
	}
}
