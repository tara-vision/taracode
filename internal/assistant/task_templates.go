package assistant

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/tara-vision/taracode/internal/storage"
	"gopkg.in/yaml.v3"
)

//go:embed templates/*.yaml
var builtinTemplates embed.FS

// TemplateLoader loads and parses task templates
type TemplateLoader struct {
	taracodeDir string
}

// NewTemplateLoader creates a new template loader
func NewTemplateLoader(taracodeDir string) *TemplateLoader {
	return &TemplateLoader{
		taracodeDir: taracodeDir,
	}
}

// ListTemplates returns all available templates (built-in + custom)
func (tl *TemplateLoader) ListTemplates() ([]TemplateInfo, error) {
	var templates []TemplateInfo

	// Load built-in templates
	entries, err := builtinTemplates.ReadDir("templates")
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".yaml") {
				name := strings.TrimSuffix(entry.Name(), ".yaml")
				templates = append(templates, TemplateInfo{
					Name:     name,
					BuiltIn:  true,
					FilePath: "templates/" + entry.Name(),
				})
			}
		}
	}

	// Load custom templates from .taracode/tasks/templates/
	customDir := filepath.Join(tl.taracodeDir, "tasks", "templates")
	if entries, err := os.ReadDir(customDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".yaml") {
				name := strings.TrimSuffix(entry.Name(), ".yaml")
				// Custom templates override built-in ones
				found := false
				for i, t := range templates {
					if t.Name == name {
						templates[i].BuiltIn = false
						templates[i].FilePath = filepath.Join(customDir, entry.Name())
						found = true
						break
					}
				}
				if !found {
					templates = append(templates, TemplateInfo{
						Name:     name,
						BuiltIn:  false,
						FilePath: filepath.Join(customDir, entry.Name()),
					})
				}
			}
		}
	}

	return templates, nil
}

// LoadTemplate loads a template by name
func (tl *TemplateLoader) LoadTemplate(name string) (*storage.TaskTemplate, error) {
	// Try custom templates first
	customPath := filepath.Join(tl.taracodeDir, "tasks", "templates", name+".yaml")
	if data, err := os.ReadFile(customPath); err == nil {
		return tl.parseTemplate(data)
	}

	// Fall back to built-in templates
	data, err := builtinTemplates.ReadFile("templates/" + name + ".yaml")
	if err != nil {
		return nil, fmt.Errorf("template not found: %s", name)
	}

	return tl.parseTemplate(data)
}

// parseTemplate parses YAML content into a TaskTemplate
func (tl *TemplateLoader) parseTemplate(data []byte) (*storage.TaskTemplate, error) {
	var template storage.TaskTemplate
	if err := yaml.Unmarshal(data, &template); err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}
	return &template, nil
}

// CreateTaskFromTemplate creates a TaskExecution from a template with variable substitution
func (tl *TemplateLoader) CreateTaskFromTemplate(template *storage.TaskTemplate, variables map[string]string) (*storage.TaskExecution, error) {
	// Merge template variables with provided variables
	allVars := make(map[string]string)
	for k, v := range template.Variables {
		allVars[k] = v
	}
	for k, v := range variables {
		allVars[k] = v
	}

	task := &storage.TaskExecution{
		ID:           uuid.New().String(),
		Name:         substituteVars(template.Name, allVars),
		Description:  substituteVars(template.Description, allVars),
		OriginalTask: "template:" + template.Name,
		Status:       storage.TaskExecStatusPending,
		CurrentStep:  -1,
		Steps:        make([]storage.TaskStep, len(template.Steps)),
	}

	for i, ts := range template.Steps {
		// Substitute variables in params
		params := make(map[string]interface{})
		for k, v := range ts.Params {
			if strVal, ok := v.(string); ok {
				params[k] = substituteVars(strVal, allVars)
			} else {
				params[k] = v
			}
		}

		// Determine action type
		actionType := storage.ActionTypeTool
		toolName := ts.Action
		command := ""

		if ts.Action == "command" || strings.HasPrefix(ts.Action, "bash:") {
			actionType = storage.ActionTypeCommand
			if cmdVal, ok := ts.Params["command"].(string); ok {
				command = substituteVars(cmdVal, allVars)
			}
		} else if ts.Action == "analyze" {
			actionType = storage.ActionTypeAnalyze
		}

		step := storage.TaskStep{
			ID:          uuid.New().String(),
			Index:       i,
			Name:        substituteVars(ts.Name, allVars),
			Description: substituteVars(ts.Name, allVars),
			Status:      storage.StepStatusPending,
			Checkpoint:  ts.Checkpoint,
			Action: storage.TaskAction{
				Type:    actionType,
				Tool:    toolName,
				Params:  params,
				Command: command,
			},
		}

		// Add verification if defined
		if ts.Verify != nil {
			step.Verification = &storage.TaskVerify{
				Command:   substituteVars(ts.Verify.Command, allVars),
				Expected:  substituteVars(ts.Verify.Contains, allVars),
				Timeout:   ts.Verify.Timeout,
				OnFailure: ts.OnFailure,
			}
			if ts.Verify.Command != "" {
				step.Verification.Type = storage.VerifyTypeCommand
			} else if ts.Verify.Contains != "" {
				step.Verification.Type = storage.VerifyTypeContains
			}
		}

		task.Steps[i] = step
	}

	return task, nil
}

// substituteVars replaces {{var}} placeholders with values
func substituteVars(s string, vars map[string]string) string {
	result := s
	for k, v := range vars {
		result = strings.ReplaceAll(result, "{{"+k+"}}", v)
	}
	return result
}

// TemplateInfo contains metadata about a template
type TemplateInfo struct {
	Name     string
	BuiltIn  bool
	FilePath string
}

// SaveCustomTemplate saves a template to the custom templates directory
func (tl *TemplateLoader) SaveCustomTemplate(template *storage.TaskTemplate) error {
	templatesDir := filepath.Join(tl.taracodeDir, "tasks", "templates")
	if err := os.MkdirAll(templatesDir, 0755); err != nil {
		return fmt.Errorf("failed to create templates directory: %w", err)
	}

	data, err := yaml.Marshal(template)
	if err != nil {
		return fmt.Errorf("failed to serialize template: %w", err)
	}

	filename := strings.ReplaceAll(strings.ToLower(template.Name), " ", "-") + ".yaml"
	path := filepath.Join(templatesDir, filename)

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write template: %w", err)
	}

	return nil
}
