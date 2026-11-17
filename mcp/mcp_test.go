package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Long-Honey-Road/Go-loom/tool"
)

type user struct{ ID, Role string }

type echoIn struct {
	Text string `json:"text" jsonschema:"what to echo"`
}

type echoOut struct {
	Text string `json:"text"`
	User string `json:"user"`
}

func tools(t *testing.T) tool.Set {
	t.Helper()
	echo := tool.MustDefine("echo", "Echo the text back.", func(ctx context.Context, in echoIn) (echoOut, error) {
		switch in.Text {
		case "missing":
			return echoOut{}, fmt.Errorf("%w: item", tool.ErrNotFound)
		case "boom":
			return echoOut{}, errors.New("dial tcp 10.0.0.7: connection refused")
		}
		tool.Emit(ctx, "echoed", in.Text)
		u, _ := tool.CallerFrom[user](ctx)
		return echoOut{Text: in.Text, User: u.ID}, nil
	}, tool.Tags("public"))
	admin := tool.MustDefine("admin_only", "Only for admins.", func(context.Context, echoIn) (echoOut, error) {
		return echoOut{}, nil
	}, tool.Tags("admin"))
	return tool.MustSet(echo, admin)
}

var byRole = tool.AllowTags(func(ctx context.Context) []string {
	u, _ := tool.CallerFrom[user](ctx)
	if u.Role == "admin" {
		return []string{"public", "admin"}
	}
	return []string{"public"}
})

func connect(t *testing.T, srv *sdk.Server) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverT, clientT := sdk.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestListsOnlyAllowedTools(t *testing.T) {
	cs := connect(t, NewServer(tools(t), byRole, user{ID: "u1", Role: "user"},
		Options{Name: "loom-test", Version: "1", Instructions: "test tools"}))

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) != 1 || res.Tools[0].Name != "echo" {
		t.Fatalf("tools = %+v, want only echo", res.Tools)
	}
	schema, _ := json.Marshal(res.Tools[0].InputSchema)
	if !strings.Contains(string(schema), `"text"`) {
		t.Fatalf("schema = %s", schema)
	}
	if init := cs.InitializeResult(); init.ServerInfo.Name != "loom-test" || init.Instructions != "test tools" {
		t.Fatalf("initialize = %+v", init)
	}
}

func TestCallRunsAsCallerAndDeliversEvents(t *testing.T) {
	var events []tool.Event
	cs := connect(t, NewServer(tools(t), byRole, user{ID: "u1", Role: "user"},
		Options{Name: "t", OnEvent: func(e tool.Event) { events = append(events, e) }}))

	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "echo", Arguments: map[string]any{"text": "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("error result: %+v", res.Content[0])
	}
	var out echoOut
	if err := json.Unmarshal([]byte(res.Content[0].(*sdk.TextContent).Text), &out); err != nil {
		t.Fatal(err)
	}
	if out != (echoOut{Text: "hi", User: "u1"}) || res.StructuredContent == nil {
		t.Fatalf("out = %+v, structured = %v", out, res.StructuredContent)
	}
	if len(events) != 1 || events[0] != (tool.Event{Tool: "echo", Type: "echoed", Data: "hi"}) {
		t.Fatalf("events = %+v", events)
	}
}

func TestCallErrors(t *testing.T) {
	cs := connect(t, NewServer(tools(t), byRole, user{ID: "u1", Role: "user"}, Options{Name: "t"}))
	tests := []struct {
		name, tool string
		args       any
		code       string
	}{
		{"wrong type", "echo", map[string]any{"text": 7}, tool.CodeInvalidArgs},
		{"no args", "echo", nil, tool.CodeInvalidArgs},
		{"not found", "echo", map[string]any{"text": "missing"}, tool.CodeNotFound},
		{"handler failure", "echo", map[string]any{"text": "boom"}, tool.CodeFailed},
		{"hidden tool", "admin_only", map[string]any{"text": "x"}, tool.CodeForbidden},
		{"unknown tool", "nope", map[string]any{}, tool.CodeUnknownTool},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: tt.tool, Arguments: tt.args})
			if err != nil {
				t.Fatal(err)
			}
			text := res.Content[0].(*sdk.TextContent).Text
			var body map[string]string
			if err := json.Unmarshal([]byte(text), &body); err != nil {
				t.Fatal(err)
			}
			if !res.IsError || body["error"] != tt.code {
				t.Fatalf("isError = %v, error = %q, want %q (%s)", res.IsError, body["error"], tt.code, text)
			}
			if strings.Contains(text, "10.0.0.7") {
				t.Fatalf("handler error leaked: %s", text)
			}
		})
	}
}
