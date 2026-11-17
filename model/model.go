// Package model is the contract between an agent and a language model: messages, tool definitions, requests,
// responses and the Model interface. Adapters live in subpackages (openai, fake); a pool of models implements
// Provider.
package model

import (
	"context"
	"encoding/json"
	"errors"
)

var (
	// ErrUnavailable means the provider failed or did not answer in time: network, 429, 5xx. A caller may retry
	// it, possibly on another model.
	ErrUnavailable = errors.New("model unavailable")
	// ErrBadResponse means the provider answered with something unusable, e.g. tool arguments that are not JSON.
	ErrBadResponse = errors.New("model returned a malformed response")
)

// Role is the author of a message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is one entry of a conversation.
type Message struct {
	Role    Role
	Content string
	// ToolCalls is set on an assistant message that calls tools.
	ToolCalls []ToolCall
	// ToolCallID is set on a tool message: the call this result answers.
	ToolCallID string
}

// ToolCall is a request of the model to run a tool with JSON arguments.
type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

// ToolDef is a tool as the model sees it: a name, what it does and the JSON Schema of its arguments.
type ToolDef struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

// Format asks for an answer in JSON matching Schema. Some models (local Qwen) never call tools while a format is
// set, so send tools and a format in separate calls.
type Format struct {
	Name   string
	Schema json.RawMessage
}

// Request is one call to a model.
type Request struct {
	Messages []Message
	Tools    []ToolDef
	Format   *Format
	// ReasoningEffort overrides the adapter's default for this call: "none", "low", "medium", "high".
	ReasoningEffort string
}

// Usage is the token count of one call.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
}

// Response is the model's message and what it cost.
type Response struct {
	Message Message
	Usage   Usage
}

// Model is a chat model with tool calling.
type Model interface {
	// Chat returns the whole answer at once.
	Chat(ctx context.Context, req Request) (Response, error)
	// Stream is Chat that calls onDelta with each piece of answer text as the model writes it. Tool calls arrive
	// assembled in the response.
	Stream(ctx context.Context, req Request, onDelta func(text string)) (Response, error)
}

// Provider hands out a model for a while, e.g. a pool of endpoints with limits on concurrent calls. A Model that
// also implements Provider is acquired by the agent once per turn, so a turn never switches models halfway.
type Provider interface {
	// Acquire blocks until a model is free or ctx ends; onWait, if not nil, is called once when it has to wait.
	// The caller must call release when done.
	Acquire(ctx context.Context, onWait func()) (m Model, release func(), err error)
}
