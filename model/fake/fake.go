// Package fake is a scripted model.Model for tests: each call gets the next step of the script, every request is
// recorded, and a step can check the request it answers. Echo answers everything with one text.
package fake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/Long-Honey-Road/Go-loom/model"
)

var (
	ErrScriptExhausted   = errors.New("fake model: script exhausted")
	ErrUnexpectedRequest = errors.New("fake model: unexpected request")
)

// Step is one scripted model turn.
type Step struct {
	reply  model.Response
	err    error
	expect func(model.Request) error
}

// Expect adds a check on the request this step answers.
func (s Step) Expect(check func(model.Request) error) Step {
	s.expect = check
	return s
}

// Client answers calls with its steps in order and records every request.
type Client struct {
	mu    sync.Mutex
	steps []Step
	calls []model.Request
}

func New(steps ...Step) *Client {
	return &Client{steps: steps}
}

func (c *Client) Chat(ctx context.Context, req model.Request) (model.Response, error) {
	if err := ctx.Err(); err != nil {
		return model.Response{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.steps) == 0 {
		return model.Response{}, ErrScriptExhausted
	}
	step := c.steps[0]
	c.steps = c.steps[1:]
	c.calls = append(c.calls, req)

	if step.expect != nil {
		if err := step.expect(req); err != nil {
			return model.Response{}, fmt.Errorf("%w: call %d: %w", ErrUnexpectedRequest, len(c.calls), err)
		}
	}
	return step.reply, step.err
}

func (c *Client) Calls() []model.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]model.Request(nil), c.calls...)
}

func (c *Client) Remaining() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.steps)
}

func Text(content string) Step {
	return assistant(model.Message{Content: content})
}

// JSON replies with v marshalled into the message content, as a model does under model.Format.
func JSON(v any) Step {
	return assistant(model.Message{Content: string(mustMarshal(v))})
}

// ToolCall replies with one call to the named tool.
func ToolCall(name string, args any) Step {
	return assistant(model.Message{ToolCalls: []model.ToolCall{{
		ID:        "call_" + name,
		Name:      name,
		Arguments: mustMarshal(args),
	}}})
}

func Fail(err error) Step {
	return Step{err: err}
}

func assistant(msg model.Message) Step {
	msg.Role = model.RoleAssistant
	return Step{reply: model.Response{Message: msg}}
}

// mustMarshal panics: a bad script is a bug in the test, not a runtime condition.
func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("fake model: marshal %T: %v", v, err))
	}
	return b
}

// WithText adds text before the tool calls, as models sometimes "think aloud" before calling a tool.
func (s Step) WithText(text string) Step {
	s.reply.Message.Content = text
	return s
}

// Stream answers like Chat and replays the text word by word through onDelta.
func (c *Client) Stream(ctx context.Context, req model.Request, onDelta func(string)) (model.Response, error) {
	resp, err := c.Chat(ctx, req)
	if err != nil {
		return resp, err
	}
	text := resp.Message.Content
	for text != "" {
		i := strings.IndexByte(text, ' ')
		if i < 0 {
			i = len(text) - 1
		}
		onDelta(text[:i+1])
		text = text[i+1:]
	}
	return resp, nil
}

var (
	_ model.Model = (*Client)(nil)
	_ model.Model = (*Echo)(nil)
)
