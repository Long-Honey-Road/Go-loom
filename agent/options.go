package agent

import (
	"time"

	"github.com/Long-Honey-Road/Go-loom/tool"
)

// Option configures an Agent in New.
type Option func(*Agent)

// Tools sets the tools the model may call. Without it the agent is a plain chat.
func Tools(s tool.Set) Option { return func(a *Agent) { a.tools = s } }

// Policy decides which tools the caller in ctx may see and call; nil allows all.
func Policy(p tool.Policy) Option { return func(a *Agent) { a.policy = p } }

// MaxToolSteps limits model calls that may request tools (default 6); the next call goes without tools and with
// a note that tools are over, so the turn always ends with an answer.
func MaxToolSteps(n int) Option { return func(a *Agent) { a.maxToolSteps = max(n, 0) } }

// Timeout limits a turn (default 80s), counted from getting a model.
func Timeout(d time.Duration) Option { return func(a *Agent) { a.timeout = d } }

// MaxRetries limits how many times checks may send the model back for another answer (default 1).
func MaxRetries(n int) Option { return func(a *Agent) { a.maxRetries = max(n, 0) } }

// LastStepNote replaces the system note sent with the call after MaxToolSteps.
func LastStepNote(note string) Option { return func(a *Agent) { a.lastStepNote = note } }

// TraceLimit cuts tool results in the trace to n bytes (default 4000); 0 keeps them whole.
func TraceLimit(n int) Option { return func(a *Agent) { a.traceLimit = n } }

// WrapModel adds middleware around every model call; the first one given is the outermost.
func WrapModel(m ...ModelMiddleware) Option {
	return func(a *Agent) { a.wrapModel = append(a.wrapModel, m...) }
}

// WrapTool adds middleware around every tool call; the first one given is the outermost.
func WrapTool(m ...ToolMiddleware) Option {
	return func(a *Agent) { a.wrapTool = append(a.wrapTool, m...) }
}

// Checks adds checks of the final answer, run in order.
func Checks(c ...Check) Option {
	return func(a *Agent) { a.checks = append(a.checks, c...) }
}
