// Package tools is the registry of what the model can call: fifteen built-in tools plus MCP tools,
// each carrying its schema, a classifier that says whether one invocation reads or mutates, and a
// context-aware executor. The registry exposes only read-form tools in investigate mode, redacts
// every output and records file mutations in the history.
package tools

import (
	"context"

	openai "github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"

	"github.com/tara-vision/taracode/internal/policy"
)

// Executor runs one invocation. ctx carries the deadline the caller chose for this call.
type Executor func(ctx context.Context, args map[string]any, workingDir string) (string, error)

// Call identifies one execution a Middleware wraps: the tool and whether it is the tool's dry run.
type Call struct {
	Tool   string
	DryRun bool
}

// Middleware wraps a tool's executor at call time. next is the tool's own Run or DryRun. A
// middleware whose executor never calls next replaces the execution (the evals replay); one that
// calls next and looks at the result observes it (the evals recorder). The registry applies it after
// the classifier and the gate have decided and before redaction, so what it returns is redacted like
// a real output.
type Middleware func(call Call, next Executor) Executor

// Param is one schema parameter. Type is string, integer or boolean.
type Param struct {
	Name        string
	Type        string
	Description string
	Enum        []string
	Required    bool
}

// Tool is one registered tool.
type Tool struct {
	Name        string
	Description string
	Params      []Param
	ReadForm    bool // has a read-only form, so it is exposed in investigate mode
	External    bool // talks to the internet; hidden when offline
	Classify    func(args map[string]any, workingDir string) policy.Invocation
	Run         Executor
	DryRun      Executor // nil when the tool has no dry run
}

// openaiTool is the schema type the llm layer sends (go-openai's, as in Phase 1).
type openaiTool = openai.Tool

// definition renders the tool's schema.
func (t *Tool) definition() openai.Tool {
	def := &jsonschema.Definition{Type: jsonschema.Object, Properties: map[string]jsonschema.Definition{}}
	for _, p := range t.Params {
		prop := jsonschema.Definition{Description: p.Description, Enum: p.Enum}
		switch p.Type {
		case "integer":
			prop.Type = jsonschema.Integer
		case "boolean":
			prop.Type = jsonschema.Boolean
		default:
			prop.Type = jsonschema.String
		}
		def.Properties[p.Name] = prop
		if p.Required {
			def.Required = append(def.Required, p.Name)
		}
	}
	return openai.Tool{Type: openai.ToolTypeFunction, Function: &openai.FunctionDefinition{
		Name: t.Name, Description: t.Description, Parameters: def}}
}
