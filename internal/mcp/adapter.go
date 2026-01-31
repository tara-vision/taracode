package mcp

import (
	"context"
	"time"

	openai "github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"
	"github.com/tara-vision/taracode/internal/tools"
)

// CreateExecutor creates a ToolExecutor for an MCP tool
func CreateExecutor(mgr *Manager, tool MCPTool) tools.ToolExecutor {
	return func(params map[string]interface{}, workingDir string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		return mgr.CallTool(ctx, tool.Name, params)
	}
}

// ToOpenAITool converts an MCPTool to an OpenAI Tool definition
func ToOpenAITool(tool MCPTool) openai.Tool {
	// Convert the input schema to jsonschema.Definition
	schemaDef := convertMCPSchema(tool.InputSchema)

	return openai.Tool{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  schemaDef,
		},
	}
}

// ToOpenAITools converts a slice of MCPTools to OpenAI Tool definitions
func ToOpenAITools(tools []MCPTool) []openai.Tool {
	result := make([]openai.Tool, len(tools))
	for i, tool := range tools {
		result[i] = ToOpenAITool(tool)
	}
	return result
}

// convertMCPSchema converts an MCP input schema to jsonschema.Definition
func convertMCPSchema(schema map[string]interface{}) *jsonschema.Definition {
	if schema == nil {
		return &jsonschema.Definition{
			Type:       jsonschema.Object,
			Properties: make(map[string]jsonschema.Definition),
		}
	}

	def := &jsonschema.Definition{
		Type: jsonschema.Object,
	}

	// Extract properties
	if props, ok := schema["properties"].(map[string]interface{}); ok {
		def.Properties = make(map[string]jsonschema.Definition)
		for name, propVal := range props {
			if propMap, ok := propVal.(map[string]interface{}); ok {
				propDef := convertPropertySchema(propMap)
				def.Properties[name] = propDef
			}
		}
	}

	// Extract required fields
	if required, ok := schema["required"].([]interface{}); ok {
		def.Required = make([]string, len(required))
		for i, r := range required {
			if s, ok := r.(string); ok {
				def.Required[i] = s
			}
		}
	} else if required, ok := schema["required"].([]string); ok {
		def.Required = required
	}

	return def
}

// convertPropertySchema converts a property schema to jsonschema.Definition
func convertPropertySchema(propMap map[string]interface{}) jsonschema.Definition {
	propDef := jsonschema.Definition{}

	// Set type
	if typeStr, ok := propMap["type"].(string); ok {
		switch typeStr {
		case "string":
			propDef.Type = jsonschema.String
		case "integer":
			propDef.Type = jsonschema.Integer
		case "number":
			propDef.Type = jsonschema.Number
		case "boolean":
			propDef.Type = jsonschema.Boolean
		case "array":
			propDef.Type = jsonschema.Array
			// Handle array items
			if items, ok := propMap["items"].(map[string]interface{}); ok {
				itemDef := convertPropertySchema(items)
				propDef.Items = &itemDef
			}
		case "object":
			propDef.Type = jsonschema.Object
			// Handle nested object properties
			if nestedProps, ok := propMap["properties"].(map[string]interface{}); ok {
				propDef.Properties = make(map[string]jsonschema.Definition)
				for name, nested := range nestedProps {
					if nestedMap, ok := nested.(map[string]interface{}); ok {
						propDef.Properties[name] = convertPropertySchema(nestedMap)
					}
				}
			}
		}
	}

	// Set description
	if desc, ok := propMap["description"].(string); ok {
		propDef.Description = desc
	}

	// Set enum
	if enumVal, ok := propMap["enum"].([]interface{}); ok {
		enumStrs := make([]string, 0, len(enumVal))
		for _, e := range enumVal {
			if s, ok := e.(string); ok {
				enumStrs = append(enumStrs, s)
			}
		}
		propDef.Enum = enumStrs
	} else if enumVal, ok := propMap["enum"].([]string); ok {
		propDef.Enum = enumVal
	}

	return propDef
}
