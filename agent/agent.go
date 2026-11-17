// Package agent runs one turn of a tool-calling agent: the model calls tools until it answers with text, under a
// step limit and a timeout, with the answer streamed, checks that may ask for another try, middleware around
// model and tool calls, and a trace of everything that happened.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Long-Honey-Road/Go-loom/model"
	"github.com/Long-Honey-Road/Go-loom/tool"
)

// ErrNoMessages: a turn needs at least one message.
var ErrNoMessages = errors.New("agent: turn has no messages")

const (
	defaultMaxToolSteps = 6
	defaultTimeout      = 80 * time.Second
	defaultMaxRetries   = 1
	defaultTraceLimit   = 4000
	defaultLastStepNote = "There are no more tool calls in this turn. Answer the user now in plain text from what " +
		"the tools have already returned."
)

// Agent is immutable and safe for concurrent turns.
type Agent struct {
	model        model.Model
	tools        tool.Set
	policy       tool.Policy
	maxToolSteps int
	timeout      time.Duration
	maxRetries   int
	lastStepNote string
	traceLimit   int
	wrapModel    []ModelMiddleware
	wrapTool     []ToolMiddleware
	checks       []Check
}

// New builds an agent over a model. If the model also implements model.Provider (a pool), every turn acquires
// one model from it for the whole turn.
func New(m model.Model, opts ...Option) *Agent {
	a := &Agent{
		model:        m,
		maxToolSteps: defaultMaxToolSteps,
		timeout:      defaultTimeout,
		maxRetries:   defaultMaxRetries,
		lastStepNote: defaultLastStepNote,
		traceLimit:   defaultTraceLimit,
	}
	for _, o := range opts {
		o(a)
	}
	return a
}

// Turn is the input of one run.
type Turn struct {
	// System is the system prompt; empty sends none.
	System string
	// Messages is the conversation so far and the new message, oldest first. The agent does not trim it.
	Messages []model.Message
	// ReasoningEffort overrides the model's default for this turn.
	ReasoningEffort string
	// Caller, if not nil, is put into ctx for policies and handlers (tool.CallerFrom).
	Caller any
	// Stream receives the turn as it happens; every field is optional.
	Stream Stream
}

// Stream is how a turn reports progress while it runs.
type Stream struct {
	// Waiting: the model is busy, the turn waits for a free one (model.Provider).
	Waiting func()
	// Delta: a piece of answer text.
	Delta func(text string)
	// Reset: forget the text streamed so far; the model went on to call tools or a check asked for another try.
	Reset func()
	// ToolCall: a tool is about to run.
	ToolCall func(call model.ToolCall)
	// Event: a tool emitted an event (tool.Emit).
	Event func(e tool.Event)
}

// Result is the output of one run.
type Result struct {
	// Text is the final answer after checks.
	Text string
	// Messages is the turn as the model saw it after Turn.Messages: assistant tool calls, tool results and the
	// final answer. Append it to the history to keep tool results, or keep only the answer.
	Messages []model.Message
	// Events are the tools' events in order.
	Events []tool.Event
	Usage  model.Usage
	// Trace is filled even when Run fails, as far as the turn got.
	Trace Trace
}

// Run executes one turn. It returns the error of the model, of a middleware, or of ctx (including the timeout);
// tool errors do not fail the turn, the model gets them as {"error", "message"} and goes on.
func (a *Agent) Run(ctx context.Context, t Turn) (Result, error) {
	if len(t.Messages) == 0 {
		return Result{}, ErrNoMessages
	}
	started := time.Now()
	res := Result{Trace: Trace{Steps: []Step{}}}
	finish := func(err error) (Result, error) {
		res.Trace.Duration = time.Since(started)
		return res, err
	}

	m := a.model
	if p, ok := a.model.(model.Provider); ok {
		held, release, err := p.Acquire(ctx, t.Stream.Waiting)
		if err != nil {
			return finish(fmt.Errorf("acquire model: %w", err))
		}
		defer release()
		m = held
	}
	// The timeout counts from getting a model, so time spent in a queue does not eat the turn.
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	if t.Caller != nil {
		ctx = tool.WithCaller(ctx, t.Caller)
	}
	ctx = tool.WithSink(ctx, func(e tool.Event) {
		res.Events = append(res.Events, e)
		call(t.Stream.Event, e)
	})

	callModel := a.modelChain(m)
	callTool := a.toolChain()
	defs := a.tools.Defs(ctx, a.policy)
	var msgs []model.Message
	if t.System != "" {
		msgs = append(msgs, model.Message{Role: model.RoleSystem, Content: t.System})
	}
	msgs = append(msgs, t.Messages...)
	sent := len(msgs)
	toolCalls, retries := 0, 0

	for step := 0; ; step++ {
		req := model.Request{ReasoningEffort: t.ReasoningEffort}
		if step < a.maxToolSteps {
			req.Tools = defs
		} else if step == a.maxToolSteps && len(defs) > 0 {
			// Told that tools are over, the model answers instead of writing a call as text.
			msgs = append(msgs, model.Message{Role: model.RoleSystem, Content: a.lastStepNote})
		}
		req.Messages = msgs

		streamed := false
		callStart := time.Now()
		resp, err := callModel(ctx, req, func(d string) {
			streamed = true
			call(t.Stream.Delta, d)
		})
		st := Step{Duration: time.Since(callStart), Usage: resp.Usage, Text: resp.Message.Content}
		res.Usage.PromptTokens += resp.Usage.PromptTokens
		res.Usage.CompletionTokens += resp.Usage.CompletionTokens
		if err != nil {
			res.Trace.Steps = append(res.Trace.Steps, st)
			return finish(fmt.Errorf("model: %w", err))
		}

		if len(resp.Message.ToolCalls) == 0 || len(req.Tools) == 0 {
			res.Trace.Steps = append(res.Trace.Steps, st)
			ans := &Answer{Text: resp.Message.Content, ToolCalls: toolCalls, Events: res.Events, Steps: res.Trace.Steps}
			retry := a.check(ctx, ans)
			if retry != "" && retries < a.maxRetries {
				retries++
				res.Trace.Notes = append(res.Trace.Notes, "retry: "+retry)
				if streamed {
					fire(t.Stream.Reset)
				}
				msgs = append(msgs, model.Message{Role: model.RoleSystem, Content: retry})
				continue
			}
			if retry != "" {
				res.Trace.Notes = append(res.Trace.Notes, "retries over, kept: "+retry)
			}
			res.Text = ans.Text
			msgs = append(msgs, model.Message{Role: model.RoleAssistant, Content: ans.Text})
			break
		}

		// Text written before tool calls is thinking aloud, not the answer.
		if streamed {
			fire(t.Stream.Reset)
		}
		msgs = append(msgs, resp.Message)
		for _, tc := range resp.Message.ToolCalls {
			toolCalls++
			call(t.Stream.ToolCall, tc)
			toolStart := time.Now()
			out, err := callTool(ctx, tc)
			code := "ok"
			if err != nil {
				if ctx.Err() != nil {
					res.Trace.Steps = append(res.Trace.Steps, st)
					return finish(fmt.Errorf("tool %s: %w", tc.Name, ctx.Err()))
				}
				code = tool.Code(err)
				out = tool.ErrorJSON(err, tc.Name)
			}
			st.Tools = append(st.Tools, ToolTrace{Name: tc.Name, Args: tc.Arguments, Code: code,
				Result: clip(string(out), a.traceLimit), Duration: time.Since(toolStart)})
			msgs = append(msgs, model.Message{Role: model.RoleTool, ToolCallID: tc.ID, Content: string(out)})
		}
		res.Trace.Steps = append(res.Trace.Steps, st)
	}
	res.Messages = msgs[sent:]
	return finish(nil)
}

// check runs every check in order; each sees the text the previous one left. Non-empty retry notes are joined.
func (a *Agent) check(ctx context.Context, ans *Answer) string {
	var notes []string
	for _, c := range a.checks {
		if note := c(ctx, ans); note != "" {
			notes = append(notes, note)
		}
	}
	return strings.Join(notes, "\n")
}

func (a *Agent) modelChain(m model.Model) ModelFunc {
	f := ModelFunc(func(ctx context.Context, req model.Request, onDelta func(string)) (model.Response, error) {
		return m.Stream(ctx, req, onDelta)
	})
	for i := len(a.wrapModel) - 1; i >= 0; i-- {
		f = a.wrapModel[i](f)
	}
	return f
}

func (a *Agent) toolChain() ToolFunc {
	f := ToolFunc(func(ctx context.Context, c model.ToolCall) (json.RawMessage, error) {
		return a.tools.Call(ctx, a.policy, c.Name, c.Arguments)
	})
	for i := len(a.wrapTool) - 1; i >= 0; i-- {
		f = a.wrapTool[i](f)
	}
	return f
}

func call[T any](f func(T), v T) {
	if f != nil {
		f(v)
	}
}

func clip(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	cut := s[:n]
	for !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + "…"
}

func fire(f func()) {
	if f != nil {
		f()
	}
}
