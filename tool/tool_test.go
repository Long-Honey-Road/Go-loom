package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

type searchIn struct {
	Query string `json:"query" jsonschema:"what to look for"`
	Limit int    `json:"limit,omitempty" jsonschema:"how many results, 1..10"`
}

type searchOut struct {
	Hits []string `json:"hits"`
}

func search(t *testing.T) Tool {
	t.Helper()
	tl, err := Define("search", "Search notes", func(ctx context.Context, in searchIn) (searchOut, error) {
		switch in.Query {
		case "secret":
			return searchOut{}, ErrForbidden
		case "nothing":
			return searchOut{}, fmt.Errorf("%w: no notes", ErrNotFound)
		case "boom":
			return searchOut{}, errors.New("db at 10.0.0.7 is down")
		case "limit":
			return searchOut{}, fmt.Errorf("%w: limit must be 1..10", ErrInvalidArgs)
		}
		Emit(ctx, "searched", in.Query)
		return searchOut{Hits: []string{in.Query}}, nil
	}, Tags("read"))
	if err != nil {
		t.Fatal(err)
	}
	return tl
}

func write(t *testing.T) Tool {
	t.Helper()
	return MustDefine("write", "Write a note", func(context.Context, searchIn) (searchOut, error) {
		return searchOut{}, nil
	}, Tags("write"))
}

func TestDefineInfersSchemaFromStruct(t *testing.T) {
	tl := search(t)
	var schema map[string]any
	if err := json.Unmarshal(tl.Parameters(), &schema); err != nil {
		t.Fatal(err)
	}
	props := schema["properties"].(map[string]any)
	if schema["type"] != "object" || props["query"] == nil || props["limit"] == nil {
		t.Fatalf("schema = %s", tl.Parameters())
	}
	if req := schema["required"].([]any); len(req) != 1 || req[0] != "query" {
		t.Fatalf("required = %v", req)
	}
	if info := tl.Info(); info.Name != "search" || info.Description != "Search notes" || !slices.Equal(info.Tags, []string{"read"}) {
		t.Fatalf("info = %+v", info)
	}
}

func TestDefineRejectsBadTools(t *testing.T) {
	if _, err := Define("", "x", func(context.Context, searchIn) (searchOut, error) { return searchOut{}, nil }); err == nil {
		t.Error("empty name accepted")
	}
	if _, err := Define("n", "x", func(context.Context, string) (searchOut, error) { return searchOut{}, nil }); err == nil {
		t.Error("non-struct arguments accepted")
	}
	defer func() {
		if recover() == nil {
			t.Error("MustDefine did not panic")
		}
	}()
	MustDefine("n", "x", func(context.Context, int) (int, error) { return 0, nil })
}

func TestSetComposition(t *testing.T) {
	s := MustSet(search(t))
	both, err := s.With(write(t))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(s.Names(), []string{"search"}) || !slices.Equal(both.Names(), []string{"search", "write"}) {
		t.Fatalf("names = %v, %v: With must not change the original", s.Names(), both.Names())
	}
	if _, err := both.With(search(t)); err == nil {
		t.Error("duplicate name accepted")
	}
	if _, err := NewSet(Tool{}); err == nil {
		t.Error("a zero Tool accepted")
	}
	if got := both.Without("write", "unknown").Names(); !slices.Equal(got, []string{"search"}) {
		t.Errorf("Without = %v", got)
	}
	if _, ok := both.Get("write"); !ok {
		t.Error("Get write")
	}
	defer func() {
		if recover() == nil {
			t.Error("MustSet did not panic")
		}
	}()
	MustSet(search(t), search(t))
}

func TestCallRunsHandlerWithEvents(t *testing.T) {
	var events []Event
	ctx := WithSink(context.Background(), func(e Event) { events = append(events, e) })
	out, err := MustSet(search(t)).Call(ctx, nil, "search", json.RawMessage(`{"query":"vpn"}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"hits":["vpn"]}` {
		t.Errorf("out = %s", out)
	}
	if len(events) != 1 || events[0] != (Event{Tool: "search", Type: "searched", Data: "vpn"}) {
		t.Errorf("events = %+v", events)
	}
	// Without a sink Emit does nothing.
	if _, err := MustSet(search(t)).Call(context.Background(), nil, "search", json.RawMessage(`{"query":"x"}`)); err != nil {
		t.Fatal(err)
	}
}

func TestCallErrorsAndExplain(t *testing.T) {
	s := MustSet(search(t), write(t))
	readOnly := AllowNames("search")
	tests := []struct {
		name, tool, args, code string
	}{
		{"unknown tool", "delete", `{}`, CodeUnknownTool},
		{"policy hides tool", "write", `{"query":"x"}`, CodeForbidden},
		{"handler forbids", "search", `{"query":"secret"}`, CodeForbidden},
		{"missing required", "search", ``, CodeInvalidArgs},
		{"wrong type", "search", `{"query":1}`, CodeInvalidArgs},
		{"extra field", "search", `{"query":"x","path":"/"}`, CodeInvalidArgs},
		{"not json", "search", `{`, CodeInvalidArgs},
		{"handler invalid", "search", `{"query":"limit"}`, CodeInvalidArgs},
		{"not found", "search", `{"query":"nothing"}`, CodeNotFound},
		{"handler fails", "search", `{"query":"boom"}`, CodeFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.Call(context.Background(), readOnly, tt.tool, json.RawMessage(tt.args))
			if Code(err) != tt.code {
				t.Fatalf("code = %s (%v), want %s", Code(err), err, tt.code)
			}
			body := string(ErrorJSON(err, tt.tool))
			if !strings.Contains(body, `"error":"`+tt.code+`"`) || strings.Contains(body, "10.0.0.7") {
				t.Fatalf("error json = %s", body)
			}
		})
	}
	if _, msg := Explain(fmt.Errorf("%w: limit must be 1..10", ErrInvalidArgs), "search"); !strings.Contains(msg, "1..10") {
		t.Errorf("invalid args message hides the reason: %q", msg)
	}
}

func TestDefsFollowPolicy(t *testing.T) {
	s := MustSet(search(t), write(t))
	if d := s.Defs(context.Background(), nil); len(d) != 2 || d[0].Name != "search" || d[0].Description != "Search notes" {
		t.Fatalf("nil policy defs = %+v", d)
	}
	if d := s.Defs(context.Background(), AllowNames("write")); len(d) != 1 || d[0].Name != "write" {
		t.Fatalf("defs = %+v", d)
	}
}

type user struct{ Role string }

func TestPolicies(t *testing.T) {
	read, wr := search(t).Info(), write(t).Info()
	public := MustDefine("ping", "", func(context.Context, searchIn) (searchOut, error) { return searchOut{}, nil }).Info()
	byRole := AllowTags(func(ctx context.Context) []string {
		if u, ok := CallerFrom[user](ctx); ok && u.Role == "admin" {
			return []string{"read", "write"}
		}
		return []string{"read"}
	})
	admin := WithCaller(context.Background(), user{Role: "admin"})
	guest := WithCaller(context.Background(), user{Role: "guest"})
	tests := []struct {
		name string
		p    Policy
		ctx  context.Context
		info Info
		want bool
	}{
		{"all", AllowAll, guest, wr, true},
		{"tags admin write", byRole, admin, wr, true},
		{"tags guest write", byRole, guest, wr, false},
		{"tags guest read", byRole, guest, read, true},
		{"tags untagged", byRole, admin, public, false},
		{"any with names", Any(byRole, AllowNames("ping")), guest, public, true},
		{"any none", Any(), guest, public, false},
		{"all both", All(byRole, AllowNames("search")), guest, read, true},
		{"all one fails", All(byRole, AllowNames("search")), admin, wr, false},
	}
	for _, tt := range tests {
		if got := tt.p.Allow(tt.ctx, tt.info); got != tt.want {
			t.Errorf("%s: Allow = %v, want %v", tt.name, got, tt.want)
		}
	}
	if _, ok := CallerFrom[user](context.Background()); ok {
		t.Error("caller found in an empty context")
	}
	if _, ok := CallerFrom[string](admin); ok {
		t.Error("caller of another type found")
	}
}
