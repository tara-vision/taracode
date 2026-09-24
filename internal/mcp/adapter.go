package mcp

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/tara-vision/taracode/internal/policy"
	"github.com/tara-vision/taracode/internal/tools"
)

const callTimeout = 60 * time.Second

// jsonTypes are the schema types the registry has no parameter type for. The model fills them as
// JSON text, which the adapter decodes back into values before the call.
var jsonTypes = map[string]bool{"number": true, "array": true, "object": true}

// ToTool adapts a discovered MCP tool to the registry. readOnly is the caller's decision (from the
// policy's mcp section: the server's readOnlyHint when trusted, else the policy's read_only list) on
// whether this tool has a read form; every other MCP tool is a mutation and hidden in investigate mode.
func ToTool(mgr *Manager, tool MCPTool, readOnly bool) *tools.Tool {
	class := policy.Mutate
	if readOnly {
		class = policy.Read
	}
	jsonArgs := jsonParams(tool.InputSchema)
	return &tools.Tool{
		Name:        tool.Name,
		Description: tool.Description,
		Params:      paramsFromSchema(tool.InputSchema),
		ReadForm:    readOnly,
		Classify: func(map[string]any, string) policy.Invocation {
			inv := policy.Invocation{Tool: tool.Name, Classification: class}
			if class == policy.Mutate {
				inv.Reason = "MCP tool " + tool.OriginalName + " on " + tool.ServerName +
					" is not a read under the policy's mcp section"
			}
			return inv
		},
		Run: func(ctx context.Context, args map[string]any, _ string) (string, error) {
			ctx, cancel := context.WithTimeout(ctx, callTimeout)
			defer cancel()
			return mgr.CallTool(ctx, tool.Name, decodeJSONArgs(args, jsonArgs))
		},
	}
}

// paramsFromSchema flattens the top level of an MCP input schema into registry params, sorted by
// name so the schema is stable. Numbers, arrays and nested objects become strings the model fills
// with JSON (the description says so); decodeJSONArgs turns them back into values.
func paramsFromSchema(schema map[string]any) []tools.Param {
	props, _ := schema["properties"].(map[string]any)
	required := map[string]bool{}
	if req, ok := schema["required"].([]any); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				required[s] = true
			}
		}
	}
	var out []tools.Param
	for name, raw := range props {
		prop, _ := raw.(map[string]any)
		typ, _ := prop["type"].(string)
		desc, _ := prop["description"].(string)
		if jsonTypes[typ] {
			desc = strings.TrimSpace(desc + " (JSON " + typ + ")")
		}
		if typ != "integer" && typ != "boolean" {
			typ = "string"
		}
		p := tools.Param{Name: name, Type: typ, Description: desc, Required: required[name]}
		if enum, ok := prop["enum"].([]any); ok {
			for _, e := range enum {
				if s, ok := e.(string); ok {
					p.Enum = append(p.Enum, s)
				}
			}
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// jsonParams names the top-level params whose schema type is a number, an array or an object.
func jsonParams(schema map[string]any) map[string]bool {
	props, _ := schema["properties"].(map[string]any)
	out := map[string]bool{}
	for name, raw := range props {
		prop, _ := raw.(map[string]any)
		if typ, _ := prop["type"].(string); jsonTypes[typ] {
			out[name] = true
		}
	}
	return out
}

// decodeJSONArgs returns a copy of args with the JSON text of the jsonArgs params decoded; a value
// that does not parse is passed on unchanged for the server to reject with its own message.
func decodeJSONArgs(args map[string]any, jsonArgs map[string]bool) map[string]any {
	out := make(map[string]any, len(args))
	for name, value := range args {
		if s, ok := value.(string); ok && jsonArgs[name] {
			var decoded any
			if json.Unmarshal([]byte(s), &decoded) == nil {
				out[name] = decoded
				continue
			}
		}
		out[name] = value
	}
	return out
}
