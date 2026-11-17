package tool

import (
	"context"
	"slices"
)

// Policy decides whether the caller in ctx may see and call a tool. The agent asks it twice: when it lists tools
// for the model and when the model calls one, so a hidden tool cannot be called by name either. Checks on the
// object a tool touches (whose file, whose record) belong in the handler: return ErrForbidden from there.
type Policy interface {
	Allow(ctx context.Context, t Info) bool
}

// PolicyFunc adapts a function to Policy.
type PolicyFunc func(ctx context.Context, t Info) bool

func (f PolicyFunc) Allow(ctx context.Context, t Info) bool { return f(ctx, t) }

// AllowAll allows every tool; a nil Policy means the same.
var AllowAll Policy = PolicyFunc(func(context.Context, Info) bool { return true })

// AllowTags allows a tool when one of its tags is among the tags returned for the caller in ctx. A tool without
// tags is never allowed by it: combine with Any for public tools.
func AllowTags(allowed func(ctx context.Context) []string) Policy {
	return PolicyFunc(func(ctx context.Context, t Info) bool {
		tags := allowed(ctx)
		return slices.ContainsFunc(t.Tags, func(tag string) bool { return slices.Contains(tags, tag) })
	})
}

// AllowNames allows exactly the named tools.
func AllowNames(names ...string) Policy {
	return PolicyFunc(func(_ context.Context, t Info) bool { return slices.Contains(names, t.Name) })
}

// Any allows a tool when at least one policy allows it.
func Any(policies ...Policy) Policy {
	return PolicyFunc(func(ctx context.Context, t Info) bool {
		return slices.ContainsFunc(policies, func(p Policy) bool { return p.Allow(ctx, t) })
	})
}

// All allows a tool only when every policy allows it.
func All(policies ...Policy) Policy {
	return PolicyFunc(func(ctx context.Context, t Info) bool {
		for _, p := range policies {
			if !p.Allow(ctx, t) {
				return false
			}
		}
		return true
	})
}
