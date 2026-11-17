// Package mcp serves a tool.Set over the Model Context Protocol, so any MCP client (Claude Code, MCP Inspector,
// another agent) gets the same tools with the same policy as the agent. Run the returned server on any transport
// of the official SDK: stdio, streamable HTTP.
package mcp

import (
	"context"
	"encoding/json"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Long-Honey-Road/Go-loom/tool"
)

// Options describe the server to clients.
type Options struct {
	Name         string
	Title        string
	Version      string
	Instructions string
	// OnEvent receives events the tools emit during calls; nil drops them.
	OnEvent func(tool.Event)
}

// NewServer lists the tools the policy allows to caller and runs calls through Set.Call with the same checks.
// A call of a tool that is not listed also reaches the set, so the client gets the set's refusal
// ({"error": "forbidden"}) rather than the SDK's "unknown tool". Errors come as {"error", "message"} with isError.
func NewServer(set tool.Set, policy tool.Policy, caller any, o Options) *sdk.Server {
	srv := sdk.NewServer(&sdk.Implementation{Name: o.Name, Title: o.Title, Version: o.Version},
		&sdk.ServerOptions{Instructions: o.Instructions})
	call := handler(set, policy, caller, o.OnEvent)
	for _, d := range set.Defs(scope(context.Background(), caller, o.OnEvent), policy) {
		srv.AddTool(&sdk.Tool{Name: d.Name, Description: d.Description, InputSchema: d.Parameters}, call)
	}
	srv.AddReceivingMiddleware(func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
			if r, ok := req.(*sdk.CallToolRequest); ok {
				return call(ctx, r)
			}
			return next(ctx, method, req)
		}
	})
	return srv
}

func handler(set tool.Set, policy tool.Policy, caller any, onEvent func(tool.Event)) sdk.ToolHandler {
	return func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		args := req.Params.Arguments
		if len(args) == 0 {
			args = json.RawMessage(`{}`)
		}
		out, err := set.Call(scope(ctx, caller, onEvent), policy, req.Params.Name, args)
		if err != nil {
			text := string(tool.ErrorJSON(err, req.Params.Name))
			return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: text}}}, nil
		}
		return &sdk.CallToolResult{
			Content:           []sdk.Content{&sdk.TextContent{Text: string(out)}},
			StructuredContent: out,
		}, nil
	}
}

func scope(ctx context.Context, caller any, onEvent func(tool.Event)) context.Context {
	if caller != nil {
		ctx = tool.WithCaller(ctx, caller)
	}
	if onEvent != nil {
		ctx = tool.WithSink(ctx, onEvent)
	}
	return ctx
}
