package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"

	"github.com/Long-Honey-Road/Go-loom/model"
)

// Set is an immutable collection of tools with unique names. Compose sets with With and Without instead of
// mutating them, so one set can be shared between agents.
type Set struct {
	tools map[string]Tool
	names []string
}

// NewSet collects tools; two tools with one name are an error.
func NewSet(tools ...Tool) (Set, error) {
	return Set{}.With(tools...)
}

// MustSet is NewSet that panics on error.
func MustSet(tools ...Tool) Set {
	s, err := NewSet(tools...)
	if err != nil {
		panic(err)
	}
	return s
}

// With returns a new set with the tools added; a name already in the set is an error.
func (s Set) With(tools ...Tool) (Set, error) {
	out := Set{tools: make(map[string]Tool, len(s.tools)+len(tools)), names: slices.Clone(s.names)}
	for name, t := range s.tools {
		out.tools[name] = t
	}
	for _, t := range tools {
		if t.call == nil {
			return Set{}, fmt.Errorf("tool %q: not built by Define", t.Name())
		}
		if _, dup := out.tools[t.Name()]; dup {
			return Set{}, fmt.Errorf("tool %q added twice", t.Name())
		}
		out.tools[t.Name()] = t
		out.names = append(out.names, t.Name())
	}
	sort.Strings(out.names)
	return out, nil
}

// Without returns a new set without the named tools; unknown names are ignored.
func (s Set) Without(names ...string) Set {
	out := Set{tools: make(map[string]Tool, len(s.tools))}
	for _, name := range s.names {
		if !slices.Contains(names, name) {
			out.tools[name] = s.tools[name]
			out.names = append(out.names, name)
		}
	}
	return out
}

// Names lists the tools in the set, sorted.
func (s Set) Names() []string { return slices.Clone(s.names) }

// Get returns the named tool.
func (s Set) Get(name string) (Tool, bool) {
	t, ok := s.tools[name]
	return t, ok
}

// Defs lists the tools the policy allows in ctx, as the model sees them. A nil policy allows everything.
func (s Set) Defs(ctx context.Context, p Policy) []model.ToolDef {
	var out []model.ToolDef
	for _, name := range s.names {
		t := s.tools[name]
		if allowed(ctx, p, t) {
			out = append(out, model.ToolDef{Name: name, Description: t.info.Description, Parameters: t.params})
		}
	}
	return out
}

// Call runs a tool the way a model asks for it: the tool must exist, the policy must allow it in ctx and the
// arguments must match the schema; only then the handler runs. The result is the handler's output as JSON.
// Errors wrap ErrUnknownTool, ErrForbidden, ErrInvalidArgs or come from the handler; see Explain.
func (s Set) Call(ctx context.Context, p Policy, name string, args json.RawMessage) (json.RawMessage, error) {
	t, ok := s.tools[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownTool, name)
	}
	if !allowed(ctx, p, t) {
		return nil, fmt.Errorf("%w: %s", ErrForbidden, name)
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	if err := t.validate(args); err != nil {
		return nil, err
	}
	out, err := t.call(withTool(ctx, name), args)
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

func allowed(ctx context.Context, p Policy, t Tool) bool {
	return p == nil || p.Allow(ctx, t.Info())
}
