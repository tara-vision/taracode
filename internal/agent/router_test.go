package agent

import (
	"testing"
)

func TestNewRouter(t *testing.T) {
	router := NewRouter(nil)

	if router == nil {
		t.Fatal("expected non-nil router")
	}
}

func TestRouterDetermineAgentType(t *testing.T) {
	router := NewRouter(nil)

	tests := []struct {
		name     string
		prompt   string
		expected Type
	}{
		// Security prompts (highest priority)
		{"security keyword", "check security issues", TypeSecurity},
		{"vulnerability keyword", "scan for vulnerabilities", TypeSecurity},
		{"audit keyword", "audit the codebase", TypeSecurity},
		{"trivy keyword", "run trivy scan", TypeSecurity},

		// DevOps prompts
		{"deploy keyword", "deploy the application", TypeDevOps},
		{"kubernetes keyword", "configure kubernetes", TypeDevOps},
		{"docker keyword", "build docker image", TypeDevOps},
		{"terraform keyword", "write terraform config", TypeDevOps},
		{"aws keyword", "deploy to aws ec2", TypeDevOps},

		// Testing prompts
		{"test keyword", "write tests for this", TypeTester},
		{"verify keyword", "verify the output", TypeTester},
		{"pytest keyword", "run pytest", TypeTester},

		// Review prompts
		{"review keyword", "review this code", TypeReviewer},
		{"code review", "can you do a code review", TypeReviewer},
		{"lint keyword", "lint the code", TypeReviewer},

		// Planning prompts
		{"plan keyword", "plan the implementation", TypePlanner},
		{"design keyword", "design a new feature", TypePlanner},
		{"architecture keyword", "describe the architecture", TypePlanner},
		{"breakdown keyword", "breakdown into subtasks", TypePlanner},

		// Diagnostics prompts
		{"debug keyword", "debug this issue", TypeDiagnostics},
		{"why keyword", "why is there an error", TypeDiagnostics},
		{"not working", "this is not working", TypeDiagnostics},
		{"problem keyword", "there's a problem", TypeDiagnostics},

		// Coding prompts
		{"implement keyword", "implement this function", TypeCoder},
		{"write keyword", "write a new class", TypeCoder},
		{"create keyword", "create a new file", TypeCoder},

		// Default to coder
		{"generic prompt", "hello world", TypeCoder},
		{"empty prompt", "", TypeCoder},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := router.DetermineAgentType(tt.prompt)
			if result != tt.expected {
				t.Errorf("expected %s for prompt %q, got %s", tt.expected, tt.prompt, result)
			}
		})
	}
}

func TestRouterExplainRouting(t *testing.T) {
	router := NewRouter(nil)

	info := router.ExplainRouting("deploy to kubernetes cluster")

	if info.MatchedAgent != TypeDevOps {
		t.Errorf("expected MatchedAgent to be DevOps, got %s", info.MatchedAgent)
	}

	// Confidence should be a string: "high", "medium", or "low"
	validConfidences := map[string]bool{"high": true, "medium": true, "low": true}
	if !validConfidences[info.Confidence] {
		t.Errorf("expected Confidence to be 'high', 'medium', or 'low', got %s", info.Confidence)
	}

	// For kubernetes, we should get high confidence
	if info.Confidence != "high" {
		t.Errorf("expected Confidence 'high' for kubernetes keyword, got %s", info.Confidence)
	}
}

func TestRouterExplainRoutingMatchedRule(t *testing.T) {
	router := NewRouter(nil)

	info := router.ExplainRouting("run trivy scan")

	if info.MatchedAgent != TypeSecurity {
		t.Errorf("expected MatchedAgent to be Security, got %s", info.MatchedAgent)
	}

	if info.MatchedRule == "" {
		t.Error("expected non-empty MatchedRule")
	}

	if info.Prompt != "run trivy scan" {
		t.Errorf("expected Prompt to be 'run trivy scan', got %s", info.Prompt)
	}
}

func TestRouterExplainRoutingLowConfidence(t *testing.T) {
	router := NewRouter(nil)

	// A prompt that doesn't strongly match any pattern
	info := router.ExplainRouting("hello there")

	if info.MatchedAgent != TypeCoder {
		t.Errorf("expected default agent to be Coder, got %s", info.MatchedAgent)
	}

	if info.Confidence != "low" {
		t.Errorf("expected Confidence 'low' for generic prompt, got %s", info.Confidence)
	}
}

func TestRoutingInfoFields(t *testing.T) {
	router := NewRouter(nil)

	info := router.ExplainRouting("write unit tests for the module")

	// Verify all fields are populated
	if info.MatchedAgent == "" {
		t.Error("expected MatchedAgent to be set")
	}

	if info.Prompt == "" {
		t.Error("expected Prompt to be set")
	}

	if info.Confidence == "" {
		t.Error("expected Confidence to be set")
	}
}

func TestRouterSetMode(t *testing.T) {
	router := NewRouter(nil)

	router.SetMode(RoutingModeManual)
	if router.GetMode() != RoutingModeManual {
		t.Errorf("expected mode to be Manual, got %s", router.GetMode())
	}

	router.SetMode(RoutingModeAuto)
	if router.GetMode() != RoutingModeAuto {
		t.Errorf("expected mode to be Auto, got %s", router.GetMode())
	}

	router.SetMode(RoutingModeTaskBased)
	if router.GetMode() != RoutingModeTaskBased {
		t.Errorf("expected mode to be TaskBased, got %s", router.GetMode())
	}
}

func TestRouterAddRule(t *testing.T) {
	router := NewRouter(nil)

	// Add a custom rule with high priority
	customRule := RoutingRule{
		Pattern:   `(?i)(custom|special)`,
		AgentType: TypeReviewer,
		Priority:  200, // Higher than any default rule
	}
	router.AddRule(customRule)

	// The custom rule should match
	result := router.DetermineAgentType("this is a custom prompt")
	if result != TypeReviewer {
		t.Errorf("expected Reviewer for custom rule, got %s", result)
	}
}

func TestRouterSetRules(t *testing.T) {
	router := NewRouter(nil)

	// Replace all rules with a single rule
	newRules := []RoutingRule{
		{Pattern: `(?i)only`, AgentType: TypeSecurity, Priority: 100},
	}
	router.SetRules(newRules)

	// Only the new rule should work
	result := router.DetermineAgentType("only this should match")
	if result != TypeSecurity {
		t.Errorf("expected Security, got %s", result)
	}

	// Other prompts should fall back to coder
	result = router.DetermineAgentType("deploy to kubernetes")
	if result != TypeCoder {
		t.Errorf("expected Coder (default) after SetRules, got %s", result)
	}
}

func TestRouterRouteByStepType(t *testing.T) {
	router := NewRouter(nil)

	tests := []struct {
		stepType    string
		description string
		expected    Type
	}{
		{"plan", "", TypePlanner},
		{"planning", "", TypePlanner},
		{"code", "", TypeCoder},
		{"coding", "", TypeCoder},
		{"implement", "", TypeCoder},
		{"edit", "", TypeCoder},
		{"test", "", TypeTester},
		{"testing", "", TypeTester},
		{"verify", "", TypeTester},
		{"review", "", TypeReviewer},
		{"reviewing", "", TypeReviewer},
		{"deploy", "", TypeDevOps},
		{"devops", "", TypeDevOps},
		{"infrastructure", "", TypeDevOps},
		{"security", "", TypeSecurity},
		{"scan", "", TypeSecurity},
		{"audit", "", TypeSecurity},
		{"diagnose", "", TypeDiagnostics},
		{"debug", "", TypeDiagnostics},
		// Unknown step type should fall back to content-based routing
		{"unknown", "deploy to kubernetes", TypeDevOps},
	}

	for _, tt := range tests {
		t.Run(tt.stepType, func(t *testing.T) {
			result := router.RouteByStepType(tt.stepType, tt.description)
			if result != tt.expected {
				t.Errorf("expected %s for step type %q, got %s", tt.expected, tt.stepType, result)
			}
		})
	}
}

func TestRouterRouteByToolName(t *testing.T) {
	router := NewRouter(nil)

	tests := []struct {
		toolName string
		expected Type
	}{
		{"trivy_scan", TypeSecurity},
		{"gitleaks_scan", TypeSecurity},
		{"kubectl_get", TypeDevOps},
		{"terraform_plan", TypeDevOps},
		{"docker_build", TypeDevOps},
		{"aws_cli", TypeDevOps},
		{"read_file", TypeCoder},
		{"git_status", TypeCoder},
		{"execute_command", TypeCoder},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			result := router.RouteByToolName(tt.toolName)
			if result != tt.expected {
				t.Errorf("expected %s for tool %q, got %s", tt.expected, tt.toolName, result)
			}
		})
	}
}

func TestDefaultRoutingRules(t *testing.T) {
	rules := DefaultRoutingRules()

	if len(rules) == 0 {
		t.Error("expected non-empty default routing rules")
	}

	// Verify rules have required fields
	for _, rule := range rules {
		if rule.Pattern == "" {
			t.Error("expected non-empty Pattern")
		}
		if rule.AgentType == "" {
			t.Error("expected non-empty AgentType")
		}
		if rule.Priority <= 0 {
			t.Error("expected positive Priority")
		}
	}
}
