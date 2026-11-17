// Package openai implements model.Model for any OpenAI-compatible chat API: OpenAI, Ollama, vLLM, OpenRouter,
// Qwen Cloud. Streaming assembles tool calls from deltas; 429, 5xx and network failures map to
// model.ErrUnavailable.
package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	oai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"

	"github.com/Long-Honey-Road/Go-loom/model"
)

type Config struct {
	BaseURL string
	Model   string
	APIKey  string
	// ReasoningEffort is sent as reasoning_effort when set; "none" turns Qwen thinking off.
	ReasoningEffort string
	// MaxRetries is the SDK retry budget for 429 and 5xx; zero means a single attempt.
	MaxRetries int
	// Temperature is sent when set; nil keeps the model default.
	Temperature *float64
}

type Client struct {
	api         oai.Client
	model       string
	effort      string
	temperature *float64
}

func New(cfg Config) *Client {
	return &Client{
		api: oai.NewClient(
			option.WithBaseURL(cfg.BaseURL),
			option.WithAPIKey(cfg.APIKey),
			option.WithMaxRetries(cfg.MaxRetries),
		),
		model:       cfg.Model,
		effort:      cfg.ReasoningEffort,
		temperature: cfg.Temperature,
	}
}

func (c *Client) Chat(ctx context.Context, req model.Request) (model.Response, error) {
	params, err := c.params(req)
	if err != nil {
		return model.Response{}, err
	}

	completion, err := c.api.Chat.Completions.New(ctx, params)
	if err != nil {
		return model.Response{}, classify(err)
	}
	if len(completion.Choices) == 0 {
		return model.Response{}, fmt.Errorf("%w: no choices", model.ErrBadResponse)
	}

	return response(completion.Choices[0].Message, completion.Usage)
}

func response(msg oai.ChatCompletionMessage, usage oai.CompletionUsage) (model.Response, error) {
	out := model.Message{Role: model.RoleAssistant, Content: msg.Content}
	for _, tc := range msg.ToolCalls {
		args := json.RawMessage(tc.Function.Arguments)
		if !json.Valid(args) {
			return model.Response{}, fmt.Errorf("%w: tool %q arguments are not JSON", model.ErrBadResponse, tc.Function.Name)
		}
		out.ToolCalls = append(out.ToolCalls, model.ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: args})
	}
	return model.Response{
		Message: out,
		Usage: model.Usage{
			PromptTokens:     int(usage.PromptTokens),
			CompletionTokens: int(usage.CompletionTokens),
		},
	}, nil
}

func (c *Client) params(req model.Request) (oai.ChatCompletionNewParams, error) {
	p := oai.ChatCompletionNewParams{Model: c.model}
	effort := c.effort
	if req.ReasoningEffort != "" {
		effort = req.ReasoningEffort
	}
	if effort != "" {
		p.ReasoningEffort = shared.ReasoningEffort(effort)
	}
	if c.temperature != nil {
		p.Temperature = oai.Float(*c.temperature)
	}

	for _, m := range req.Messages {
		msg, err := message(m)
		if err != nil {
			return p, err
		}
		p.Messages = append(p.Messages, msg)
	}

	for _, t := range req.Tools {
		var schema shared.FunctionParameters
		if err := json.Unmarshal(t.Parameters, &schema); err != nil {
			return p, fmt.Errorf("tool %q parameters: %w", t.Name, err)
		}
		p.Tools = append(p.Tools, oai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        t.Name,
			Description: oai.String(t.Description),
			Parameters:  schema,
		}))
	}

	if req.Format != nil {
		p.ResponseFormat = oai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{
				JSONSchema: shared.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:   req.Format.Name,
					Strict: oai.Bool(true),
					Schema: req.Format.Schema,
				},
			},
		}
	}
	return p, nil
}

func message(m model.Message) (oai.ChatCompletionMessageParamUnion, error) {
	switch m.Role {
	case model.RoleSystem:
		return oai.SystemMessage(m.Content), nil
	case model.RoleUser:
		return oai.UserMessage(m.Content), nil
	case model.RoleTool:
		return oai.ToolMessage(m.Content, m.ToolCallID), nil
	case model.RoleAssistant:
		a := oai.ChatCompletionAssistantMessageParam{}
		if m.Content != "" {
			a.Content.OfString = oai.String(m.Content)
		}
		for _, tc := range m.ToolCalls {
			a.ToolCalls = append(a.ToolCalls, oai.ChatCompletionMessageToolCallUnionParam{
				OfFunction: &oai.ChatCompletionMessageFunctionToolCallParam{
					ID: tc.ID,
					Function: oai.ChatCompletionMessageFunctionToolCallFunctionParam{
						Name:      tc.Name,
						Arguments: string(tc.Arguments),
					},
				},
			})
		}
		return oai.ChatCompletionMessageParamUnion{OfAssistant: &a}, nil
	default:
		return oai.ChatCompletionMessageParamUnion{}, fmt.Errorf("unknown message role %q", m.Role)
	}
}

// classify marks failures worth retrying or escalating as model.ErrUnavailable.
func classify(err error) error {
	var apiErr *oai.Error
	if errors.As(err, &apiErr) {
		if apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode >= http.StatusInternalServerError {
			return fmt.Errorf("%w: %w", model.ErrUnavailable, err)
		}
		return fmt.Errorf("model request rejected: %w", err)
	}
	return fmt.Errorf("%w: %w", model.ErrUnavailable, err)
}

// Stream streams the answer; text deltas go to onDelta, tool calls arrive assembled in the response.
func (c *Client) Stream(ctx context.Context, req model.Request, onDelta func(string)) (model.Response, error) {
	params, err := c.params(req)
	if err != nil {
		return model.Response{}, err
	}
	params.StreamOptions = oai.ChatCompletionStreamOptionsParam{IncludeUsage: oai.Bool(true)}

	stream := c.api.Chat.Completions.NewStreaming(ctx, params)
	defer stream.Close()
	var acc oai.ChatCompletionAccumulator
	for stream.Next() {
		chunk := stream.Current()
		acc.AddChunk(chunk)
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			onDelta(chunk.Choices[0].Delta.Content)
		}
	}
	if err := stream.Err(); err != nil {
		return model.Response{}, classify(err)
	}
	if len(acc.Choices) == 0 {
		return model.Response{}, fmt.Errorf("%w: empty stream", model.ErrBadResponse)
	}
	return response(acc.Choices[0].Message, acc.Usage)
}

var _ model.Model = (*Client)(nil)
