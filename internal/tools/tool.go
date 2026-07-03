// Package tools defines the agent's tool surface: a Tool interface, a registry,
// and the built-in tools (bash, file ops, search). Tools are provider-agnostic;
// the agent passes Registry.Specs() to any ai.Provider and dispatches tool
// calls back through Registry.Get.
package tools

import (
	"context"
	"encoding/json"

	"spaish/internal/ai"
)

// Tool is a single capability the model can invoke.
type Tool interface {
	Name() string
	Description() string
	// Schema returns the JSON Schema (an "object" schema) for the tool input.
	Schema() map[string]any
	// Run executes the tool with the raw JSON input and returns output text.
	// A non-nil error is surfaced to the model as an error tool result.
	Run(ctx context.Context, input json.RawMessage) (string, error)
}

// Registry holds the available tools in a stable order.
type Registry struct {
	tools map[string]Tool
	order []string
}

// NewRegistry builds a registry from the given tools, preserving order.
func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{tools: make(map[string]Tool, len(tools))}
	for _, t := range tools {
		if _, exists := r.tools[t.Name()]; exists {
			continue
		}
		r.tools[t.Name()] = t
		r.order = append(r.order, t.Name())
	}
	return r
}

// DefaultRegistry returns the standard built-in tool set.
func DefaultRegistry() *Registry {
	return NewRegistry(
		&Bash{},
		&ReadFile{},
		&WriteFile{},
		&EditFile{},
		&Glob{},
		&Grep{},
		&ListDir{},
		&WebFetch{},
		&ApplyPatch{},
		&HTTPRequest{},
		&MultiEdit{},
	)
}

// Get returns the tool registered under name.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Specs returns the tool specifications to advertise to a provider.
func (r *Registry) Specs() []ai.ToolSpec {
	specs := make([]ai.ToolSpec, 0, len(r.order))
	for _, name := range r.order {
		t := r.tools[name]
		specs = append(specs, ai.ToolSpec{
			Name:        t.Name(),
			Description: t.Description(),
			Schema:      t.Schema(),
		})
	}
	return specs
}

// Add appends tools to the registry, preserving order and keeping the dedupe
// behavior of NewRegistry: a tool whose name is already registered is skipped.
// Used to extend the built-in set with dynamically discovered tools (e.g. MCP).
func (r *Registry) Add(tools ...Tool) {
	for _, t := range tools {
		if _, exists := r.tools[t.Name()]; exists {
			continue
		}
		r.tools[t.Name()] = t
		r.order = append(r.order, t.Name())
	}
}

// objectSchema is a small helper for building an object JSON schema.
func objectSchema(props map[string]any, required ...string) map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": props,
		"required":   required,
	}
}

// strProp builds a string property schema with a description.
func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

// PathArg extracts the "path" field from a tool call input, for display and
// permission classification. Returns "" when absent.
func PathArg(input json.RawMessage) string {
	var args struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(input, &args)
	return args.Path
}

// URLArg extracts the "url" field from a tool call input, for display in
// the confirmation prompt. Returns "" when absent.
func URLArg(input json.RawMessage) string {
	var args struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal(input, &args)
	return args.URL
}

// tailTrim returns the last maxBytes bytes of s, prefixed with a notice when
// truncation occurs. Tool output is capped to keep the model's context bounded.
func tailTrim(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	return "[...output truncated...]\n" + s[len(s)-maxBytes:]
}
