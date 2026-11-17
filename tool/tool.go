// Package tool defines typed tools for a model, sets of them, the policy that decides who may call what, and
// events a tool sends to the outside world while it runs.
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/google/jsonschema-go/jsonschema"
)

// Tool is a named action with a JSON Schema of its arguments, built by Define.
type Tool struct {
	info     Info
	params   json.RawMessage
	resolved *jsonschema.Resolved
	call     func(ctx context.Context, args json.RawMessage) (any, error)
}

// Info is what a policy sees of a tool.
type Info struct {
	Name        string
	Description string
	Tags        []string
}

// Option sets a property of a tool in Define.
type Option func(*Info)

// Tags labels a tool for policies, e.g. Tags("fs", "write").
func Tags(tags ...string) Option {
	return func(i *Info) { i.Tags = append(i.Tags, tags...) }
}

// Define builds a tool from a typed handler. The argument schema is inferred from In: its json tags name the
// fields, jsonschema tags describe them for the model, fields without omitempty are required and unknown fields
// are rejected. In must be a struct.
func Define[In, Out any](name, description string, fn func(ctx context.Context, in In) (Out, error),
	opts ...Option,
) (Tool, error) {
	if name == "" {
		return Tool{}, fmt.Errorf("tool: empty name")
	}
	schema, err := jsonschema.For[In](nil)
	if err != nil {
		return Tool{}, fmt.Errorf("tool %q schema: %w", name, err)
	}
	if schema.Type != "object" {
		return Tool{}, fmt.Errorf("tool %q: arguments must be a struct, got %q", name, schema.Type)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return Tool{}, fmt.Errorf("tool %q resolve schema: %w", name, err)
	}
	params, err := json.Marshal(schema)
	if err != nil {
		return Tool{}, fmt.Errorf("tool %q marshal schema: %w", name, err)
	}
	info := Info{Name: name, Description: description}
	for _, o := range opts {
		o(&info)
	}
	return Tool{
		info:     info,
		params:   params,
		resolved: resolved,
		call: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var in In
			if err := json.Unmarshal(raw, &in); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInvalidArgs, err)
			}
			return fn(ctx, in)
		},
	}, nil
}

// MustDefine is Define that panics on error, for tools declared as package variables.
func MustDefine[In, Out any](name, description string, fn func(ctx context.Context, in In) (Out, error),
	opts ...Option,
) Tool {
	t, err := Define(name, description, fn, opts...)
	if err != nil {
		panic(err)
	}
	return t
}

// Name is the tool's name as the model calls it.
func (t Tool) Name() string { return t.info.Name }

// Info returns the tool's name, description and tags.
func (t Tool) Info() Info {
	i := t.info
	i.Tags = slices.Clone(i.Tags)
	return i
}

// Parameters is the JSON Schema of the arguments.
func (t Tool) Parameters() json.RawMessage { return t.params }

// validate checks raw arguments against the schema before the handler sees them.
func (t Tool) validate(raw json.RawMessage) error {
	var instance any
	if err := json.Unmarshal(raw, &instance); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidArgs, err)
	}
	if err := t.resolved.Validate(instance); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidArgs, err)
	}
	return nil
}
