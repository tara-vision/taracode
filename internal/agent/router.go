package agent

import (
	"regexp"
	"strings"
)

// RoutingMode defines how agents are selected for tasks
type RoutingMode string

const (
	RoutingModeAuto      RoutingMode = "auto"       // System decides based on task content
	RoutingModeManual    RoutingMode = "manual"     // User specifies agent
	RoutingModeTaskBased RoutingMode = "task-based" // Based on task step type
)

// RoutingRule defines a rule for routing tasks to agents
type RoutingRule struct {
	Pattern   string `yaml:"pattern" json:"pattern"`     // Regex pattern to match
	AgentType Type   `yaml:"agent_type" json:"agent_type"` // Agent to route to
	Priority  int    `yaml:"priority" json:"priority"`   // Higher = checked first
}

// Router handles agent selection based on task content and routing rules
type Router struct {
	registry *Registry
	rules    []RoutingRule
	mode     RoutingMode
}

// NewRouter creates a new agent router
func NewRouter(registry *Registry) *Router {
	return &Router{
		registry: registry,
		rules:    DefaultRoutingRules(),
		mode:     RoutingModeAuto,
	}
}

// DefaultRoutingRules returns the default routing rules
func DefaultRoutingRules() []RoutingRule {
	return []RoutingRule{
		// Security-related patterns
		{Pattern: `(?i)(security|vulnerability|scan|audit|secret|credential)`, AgentType: TypeSecurity, Priority: 100},
		{Pattern: `(?i)(trivy|gitleaks|sast|tfsec|kubesec)`, AgentType: TypeSecurity, Priority: 100},

		// DevOps patterns
		{Pattern: `(?i)(kubernetes|k8s|kubectl|helm|pod|deployment|service)`, AgentType: TypeDevOps, Priority: 90},
		{Pattern: `(?i)(terraform|tf|infrastructure|provision)`, AgentType: TypeDevOps, Priority: 90},
		{Pattern: `(?i)(docker|container|image|dockerfile|compose)`, AgentType: TypeDevOps, Priority: 90},
		{Pattern: `(?i)(aws|azure|gcp|cloud|ec2|s3|eks|aks|gke)`, AgentType: TypeDevOps, Priority: 90},
		{Pattern: `(?i)(deploy|rollout|release|ci.?cd)`, AgentType: TypeDevOps, Priority: 85},

		// Testing patterns
		{Pattern: `(?i)(test|spec|verify|validate|assert)`, AgentType: TypeTester, Priority: 80},
		{Pattern: `(?i)(pytest|jest|mocha|go test|cargo test)`, AgentType: TypeTester, Priority: 80},

		// Review patterns
		{Pattern: `(?i)(review|check|inspect|quality|lint)`, AgentType: TypeReviewer, Priority: 70},
		{Pattern: `(?i)(code review|pr review|pull request)`, AgentType: TypeReviewer, Priority: 75},

		// Planning patterns
		{Pattern: `(?i)(plan|design|architect|breakdown|decompose)`, AgentType: TypePlanner, Priority: 60},
		{Pattern: `(?i)(how (should|do) (i|we)|what steps|approach)`, AgentType: TypePlanner, Priority: 55},

		// Diagnostic patterns
		{Pattern: `(?i)(debug|diagnose|why|error|fail|fix|broken)`, AgentType: TypeDiagnostics, Priority: 50},
		{Pattern: `(?i)(not working|doesn't work|issue|problem)`, AgentType: TypeDiagnostics, Priority: 50},

		// Coding patterns (default fallback)
		{Pattern: `(?i)(write|create|implement|add|edit|modify|update|code)`, AgentType: TypeCoder, Priority: 40},
		{Pattern: `(?i)(function|class|method|file|module)`, AgentType: TypeCoder, Priority: 35},
	}
}

// SetMode sets the routing mode
func (r *Router) SetMode(mode RoutingMode) {
	r.mode = mode
}

// GetMode returns the current routing mode
func (r *Router) GetMode() RoutingMode {
	return r.mode
}

// AddRule adds a custom routing rule
func (r *Router) AddRule(rule RoutingRule) {
	r.rules = append(r.rules, rule)
	// Sort by priority (higher first)
	r.sortRules()
}

// SetRules replaces all routing rules
func (r *Router) SetRules(rules []RoutingRule) {
	r.rules = rules
	r.sortRules()
}

// sortRules sorts rules by priority (descending)
func (r *Router) sortRules() {
	// Simple bubble sort for small rule sets
	for i := 0; i < len(r.rules)-1; i++ {
		for j := 0; j < len(r.rules)-i-1; j++ {
			if r.rules[j].Priority < r.rules[j+1].Priority {
				r.rules[j], r.rules[j+1] = r.rules[j+1], r.rules[j]
			}
		}
	}
}

// Route determines the best agent for a given prompt
func (r *Router) Route(prompt string) (Agent, error) {
	agentType := r.DetermineAgentType(prompt)
	return r.registry.Get(agentType)
}

// DetermineAgentType returns the best agent type for a given prompt
func (r *Router) DetermineAgentType(prompt string) Type {
	prompt = strings.ToLower(prompt)

	// Check rules in priority order
	for _, rule := range r.rules {
		matched, err := regexp.MatchString(rule.Pattern, prompt)
		if err == nil && matched {
			return rule.AgentType
		}
	}

	// Default to coder
	return TypeCoder
}

// RouteByStepType determines the agent for a task step
func (r *Router) RouteByStepType(stepType string, stepDescription string) Type {
	stepType = strings.ToLower(stepType)

	// First check explicit step type
	switch stepType {
	case "plan", "planning":
		return TypePlanner
	case "code", "coding", "implement", "edit":
		return TypeCoder
	case "test", "testing", "verify":
		return TypeTester
	case "review", "reviewing":
		return TypeReviewer
	case "deploy", "devops", "infrastructure":
		return TypeDevOps
	case "security", "scan", "audit":
		return TypeSecurity
	case "diagnose", "debug":
		return TypeDiagnostics
	}

	// Fall back to content-based routing
	return r.DetermineAgentType(stepDescription)
}

// RouteByToolName determines the agent for a specific tool
func (r *Router) RouteByToolName(toolName string) Type {
	category := GetToolCategory(toolName)

	switch category {
	case ToolCategorySecurity:
		return TypeSecurity
	case ToolCategoryKubernetes, ToolCategoryTerraform, ToolCategoryDocker, ToolCategoryCloud:
		return TypeDevOps
	case ToolCategoryFile, ToolCategoryGit:
		return TypeCoder
	case ToolCategoryCommand:
		// Commands could be for testing or general execution
		return TypeCoder
	default:
		return TypeCoder
	}
}

// GetRoutingInfo returns information about how a prompt would be routed
type RoutingInfo struct {
	Prompt        string `json:"prompt"`
	MatchedRule   string `json:"matched_rule,omitempty"`
	MatchedAgent  Type   `json:"matched_agent"`
	Confidence    string `json:"confidence"` // high, medium, low
	Alternatives  []Type `json:"alternatives,omitempty"`
}

// ExplainRouting explains how a prompt would be routed
func (r *Router) ExplainRouting(prompt string) RoutingInfo {
	info := RoutingInfo{
		Prompt:     prompt,
		Confidence: "low",
	}

	prompt = strings.ToLower(prompt)
	var matchedRules []RoutingRule

	// Find all matching rules
	for _, rule := range r.rules {
		matched, err := regexp.MatchString(rule.Pattern, prompt)
		if err == nil && matched {
			matchedRules = append(matchedRules, rule)
		}
	}

	if len(matchedRules) > 0 {
		// Use highest priority match
		bestRule := matchedRules[0]
		info.MatchedAgent = bestRule.AgentType
		info.MatchedRule = bestRule.Pattern

		// Determine confidence based on priority
		if bestRule.Priority >= 90 {
			info.Confidence = "high"
		} else if bestRule.Priority >= 60 {
			info.Confidence = "medium"
		}

		// Add alternatives from other matching rules
		seen := map[Type]bool{bestRule.AgentType: true}
		for _, rule := range matchedRules[1:] {
			if !seen[rule.AgentType] {
				info.Alternatives = append(info.Alternatives, rule.AgentType)
				seen[rule.AgentType] = true
			}
		}
	} else {
		// Default to coder
		info.MatchedAgent = TypeCoder
	}

	return info
}
