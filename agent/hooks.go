package agent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Long-Honey-Road/Go-loom/model"
	"github.com/Long-Honey-Road/Go-loom/tool"
)

// ModelFunc is one model call inside a turn.
type ModelFunc func(ctx context.Context, req model.Request, onDelta func(text string)) (model.Response, error)

// ModelMiddleware wraps model calls: change the request, look at the response, retry, log, count tokens.
type ModelMiddleware func(next ModelFunc) ModelFunc

// ToolFunc is one tool call inside a turn; it returns the result as JSON or an error the model will see as a code.
type ToolFunc func(ctx context.Context, call model.ToolCall) (json.RawMessage, error)

// ToolMiddleware wraps tool calls: audit, ask a human to confirm, limit rate, cache. Return tool.ErrForbidden to
// refuse a call; the model gets "forbidden" and goes on.
type ToolMiddleware func(next ToolFunc) ToolFunc

// Answer is the model's final text as checks see it.
type Answer struct {
	// Text may be rewritten by a check; the next check and the result get the new text.
	Text string
	// ToolCalls is how many tools ran in this turn so far.
	ToolCalls int
	// Events are the tools' events so far.
	Events []tool.Event
	// Steps are the model calls so far.
	Steps []Step
}

// Check looks at the final answer. It may rewrite Answer.Text (drop invented links, redact), or return a note
// that sends the model back for another answer with this note as a system message, e.g. "you answered without
// searching: search first". Retries are limited by MaxRetries; after that the answer is kept as is.
type Check func(ctx context.Context, a *Answer) (retry string)

// Trace is how the turn went, for logs and debugging.
type Trace struct {
	Steps []Step `json:"steps"`
	// Notes are the agent's remarks: retries asked by checks and whether they were granted.
	Notes    []string      `json:"notes,omitempty"`
	Duration time.Duration `json:"duration"`
}

// Step is one model call with the tools it asked for.
type Step struct {
	Duration time.Duration `json:"duration"`
	Usage    model.Usage   `json:"usage"`
	// Text is what the model wrote in this call, also text it wrote before calling tools.
	Text  string      `json:"text,omitempty"`
	Tools []ToolTrace `json:"tools,omitempty"`
}

// ToolTrace is one tool call.
type ToolTrace struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
	// Code is "ok" or an error code of tool.Code.
	Code string `json:"code"`
	// Result is the JSON the model got, cut to TraceLimit.
	Result   string        `json:"result"`
	Duration time.Duration `json:"duration"`
}
