package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Long-Honey-Road/Go-loom/model"
	"github.com/Long-Honey-Road/Go-loom/model/fake"
	"github.com/Long-Honey-Road/Go-loom/tool"
)

type user struct{ ID string }

type findIn struct {
	Query string `json:"query"`
}

type findOut struct {
	Hits []string `json:"hits"`
	By   string   `json:"by,omitempty"`
}

func tools(t *testing.T) tool.Set {
	t.Helper()
	find := tool.MustDefine("find", "Find notes", func(ctx context.Context, in findIn) (findOut, error) {
		if in.Query == "boom" {
			return findOut{}, errors.New("disk at /dev/sda1 failed")
		}
		tool.Emit(ctx, "found", in.Query)
		u, _ := tool.CallerFrom[user](ctx)
		return findOut{Hits: []string{in.Query + " note"}, By: u.ID}, nil
	}, tool.Tags("read"))
	erase := tool.MustDefine("erase", "Erase notes", func(context.Context, findIn) (findOut, error) {
		return findOut{}, nil
	}, tool.Tags("write"))
	return tool.MustSet(find, erase)
}

func ask(text string) Turn {
	return Turn{System: "be brief", Messages: []model.Message{{Role: model.RoleUser, Content: text}}}
}

// recorder keeps what a turn streamed.
type recorder struct {
	text   strings.Builder
	resets int
	calls  []string
	events []tool.Event
	waits  int
}

func (r *recorder) stream() Stream {
	return Stream{
		Waiting:  func() { r.waits++ },
		Delta:    func(d string) { r.text.WriteString(d) },
		Reset:    func() { r.resets++; r.text.Reset() },
		ToolCall: func(c model.ToolCall) { r.calls = append(r.calls, c.Name) },
		Event:    func(e tool.Event) { r.events = append(r.events, e) },
	}
}

func TestPlainAnswerIsStreamed(t *testing.T) {
	m := fake.New(fake.Text("Привет, чем помочь?").Expect(func(req model.Request) error {
		if len(req.Messages) != 2 || req.Messages[0].Role != model.RoleSystem || req.ReasoningEffort != "low" {
			return fmt.Errorf("request = %+v", req)
		}
		return nil
	}))
	var r recorder
	turn := ask("привет")
	turn.Stream, turn.ReasoningEffort = r.stream(), "low"

	res, err := New(m).Run(context.Background(), turn)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Привет, чем помочь?" || r.text.String() != res.Text {
		t.Fatalf("text = %q, streamed %q", res.Text, r.text.String())
	}
	if len(res.Messages) != 1 || res.Messages[0].Role != model.RoleAssistant || len(res.Trace.Steps) != 1 {
		t.Fatalf("messages = %+v, steps = %+v", res.Messages, res.Trace.Steps)
	}
	if req := m.Calls()[0]; len(req.Tools) != 0 {
		t.Fatalf("tools offered without a set: %+v", req.Tools)
	}
}

func TestToolCallThenAnswer(t *testing.T) {
	m := fake.New(
		fake.ToolCall("find", findIn{Query: "vpn"}).WithText("Сейчас поищу."),
		fake.Text("Нашёл заметку про VPN.").Expect(func(req model.Request) error {
			last := req.Messages[len(req.Messages)-1]
			if last.Role != model.RoleTool || last.ToolCallID != "call_find" || !strings.Contains(last.Content, "vpn note") {
				return fmt.Errorf("last message = %+v", last)
			}
			return nil
		}),
	)
	var r recorder
	turn := ask("что у меня про vpn?")
	turn.Stream, turn.Caller = r.stream(), user{ID: "u1"}

	res, err := New(m, Tools(tools(t))).Run(context.Background(), turn)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Нашёл заметку про VPN." || r.text.String() != res.Text || r.resets != 1 {
		t.Fatalf("text = %q, streamed = %q, resets = %d", res.Text, r.text.String(), r.resets)
	}
	if !slices.Equal(r.calls, []string{"find"}) || len(res.Events) != 1 || res.Events[0].Type != "found" ||
		!slices.Equal(r.events, res.Events) {
		t.Fatalf("calls = %v, events = %+v / %+v", r.calls, res.Events, r.events)
	}
	if len(res.Messages) != 3 || res.Messages[0].Role != model.RoleAssistant || res.Messages[1].Role != model.RoleTool {
		t.Fatalf("messages = %+v", res.Messages)
	}
	if !strings.Contains(res.Messages[1].Content, `"by":"u1"`) {
		t.Fatalf("caller did not reach the handler: %s", res.Messages[1].Content)
	}
	tr := res.Trace.Steps[0].Tools[0]
	if tr.Name != "find" || tr.Code != "ok" || string(tr.Args) != `{"query":"vpn"}` || res.Trace.Steps[0].Text != "Сейчас поищу." {
		t.Fatalf("trace = %+v", res.Trace.Steps)
	}
}

func TestToolErrorGoesToModel(t *testing.T) {
	m := fake.New(
		fake.ToolCall("find", findIn{Query: "boom"}),
		fake.Text("Поиск не работает."),
	)
	res, err := New(m, Tools(tools(t))).Run(context.Background(), ask("найди"))
	if err != nil {
		t.Fatal(err)
	}
	got := m.Calls()[1].Messages
	content := got[len(got)-1].Content
	if !strings.Contains(content, `"error":"tool_failed"`) || strings.Contains(content, "/dev/sda1") {
		t.Fatalf("tool message = %s", content)
	}
	if res.Trace.Steps[0].Tools[0].Code != tool.CodeFailed {
		t.Fatalf("trace = %+v", res.Trace.Steps[0].Tools)
	}
}

func TestPolicyHidesAndRefusesTools(t *testing.T) {
	m := fake.New(
		fake.ToolCall("erase", findIn{Query: "all"}).Expect(func(req model.Request) error {
			if len(req.Tools) != 1 || req.Tools[0].Name != "find" {
				return fmt.Errorf("offered %+v", req.Tools)
			}
			return nil
		}),
		fake.Text("Не могу стереть."),
	)
	_, err := New(m, Tools(tools(t)), Policy(tool.AllowNames("find"))).Run(context.Background(), ask("сотри всё"))
	if err != nil {
		t.Fatal(err)
	}
	got := m.Calls()[1].Messages
	if c := got[len(got)-1].Content; !strings.Contains(c, `"error":"forbidden"`) {
		t.Fatalf("hidden tool ran: %s", c)
	}
}

func TestStepLimitEndsWithAnswer(t *testing.T) {
	m := fake.New(
		fake.ToolCall("find", findIn{Query: "a"}),
		fake.ToolCall("find", findIn{Query: "b"}),
		fake.Text("Хватит поисков.").Expect(func(req model.Request) error {
			last := req.Messages[len(req.Messages)-1]
			if len(req.Tools) != 0 || last.Role != model.RoleSystem || last.Content != "stop" {
				return fmt.Errorf("tools = %d, last = %+v", len(req.Tools), last)
			}
			return nil
		}),
	)
	res, err := New(m, Tools(tools(t)), MaxToolSteps(2), LastStepNote("stop")).Run(context.Background(), ask("ищи"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Хватит поисков." || len(res.Trace.Steps) != 3 {
		t.Fatalf("text = %q, steps = %d", res.Text, len(res.Trace.Steps))
	}
}

func TestToolCallsWithoutToolsOfferedAreIgnored(t *testing.T) {
	m := fake.New(fake.ToolCall("find", findIn{Query: "a"}).WithText("Ответ."))
	res, err := New(m, Tools(tools(t)), MaxToolSteps(0)).Run(context.Background(), ask("x"))
	if err != nil || res.Text != "Ответ." {
		t.Fatalf("text = %q, err = %v", res.Text, err)
	}
}

func TestChecksRewriteAndRetry(t *testing.T) {
	needsTools := func(_ context.Context, a *Answer) string {
		if a.ToolCalls == 0 {
			return "search first"
		}
		return ""
	}
	redact := func(_ context.Context, a *Answer) string {
		a.Text = strings.ReplaceAll(a.Text, "secret", "***")
		return ""
	}
	m := fake.New(
		fake.Text("Из головы."),
		fake.ToolCall("find", findIn{Query: "x"}).Expect(func(req model.Request) error {
			if last := req.Messages[len(req.Messages)-1]; last.Role != model.RoleSystem || last.Content != "search first" {
				return fmt.Errorf("last = %+v", last)
			}
			return nil
		}),
		fake.Text("Нашёл secret."),
	)
	var r recorder
	turn := ask("x")
	turn.Stream = r.stream()
	res, err := New(m, Tools(tools(t)), Checks(needsTools, redact)).Run(context.Background(), turn)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Нашёл ***." || r.resets != 1 || len(res.Trace.Notes) != 1 {
		t.Fatalf("text = %q, resets = %d, notes = %v", res.Text, r.resets, res.Trace.Notes)
	}
}

func TestRetriesAreLimited(t *testing.T) {
	always := func(context.Context, *Answer) string { return "again" }
	m := fake.New(fake.Text("один"), fake.Text("два"))
	res, err := New(m, Checks(always), MaxRetries(1)).Run(context.Background(), ask("x"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "два" || m.Remaining() != 0 || len(res.Trace.Notes) != 2 {
		t.Fatalf("text = %q, notes = %v", res.Text, res.Trace.Notes)
	}
}

func TestMiddlewareOrder(t *testing.T) {
	var order []string
	mw := func(name string) ModelMiddleware {
		return func(next ModelFunc) ModelFunc {
			return func(ctx context.Context, req model.Request, d func(string)) (model.Response, error) {
				order = append(order, name)
				return next(ctx, req, d)
			}
		}
	}
	confirm := func(next ToolFunc) ToolFunc {
		return func(ctx context.Context, c model.ToolCall) (json.RawMessage, error) {
			order = append(order, "tool:"+c.Name)
			if c.Name == "erase" {
				return nil, tool.ErrForbidden
			}
			return next(ctx, c)
		}
	}
	m := fake.New(fake.ToolCall("erase", findIn{Query: "x"}), fake.Text("ok"))
	res, err := New(m, Tools(tools(t)), WrapModel(mw("outer"), mw("inner")), WrapTool(confirm)).
		Run(context.Background(), ask("x"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"outer", "inner", "tool:erase", "outer", "inner"}; !slices.Equal(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	if res.Trace.Steps[0].Tools[0].Code != tool.CodeForbidden {
		t.Fatalf("trace = %+v", res.Trace.Steps[0].Tools)
	}
}

func TestModelErrorFailsTurnWithTrace(t *testing.T) {
	m := fake.New(fake.ToolCall("find", findIn{Query: "x"}), fake.Fail(model.ErrUnavailable))
	res, err := New(m, Tools(tools(t))).Run(context.Background(), ask("x"))
	if !errors.Is(err, model.ErrUnavailable) || len(res.Trace.Steps) != 2 || res.Trace.Duration == 0 {
		t.Fatalf("err = %v, steps = %d", err, len(res.Trace.Steps))
	}
}

func TestEmptyTurn(t *testing.T) {
	if _, err := New(fake.New()).Run(context.Background(), Turn{System: "x"}); !errors.Is(err, ErrNoMessages) {
		t.Fatalf("err = %v", err)
	}
}

// slow blocks until its context ends.
type slow struct{}

func (slow) Chat(ctx context.Context, _ model.Request) (model.Response, error) {
	<-ctx.Done()
	return model.Response{}, ctx.Err()
}

func (s slow) Stream(ctx context.Context, req model.Request, _ func(string)) (model.Response, error) {
	return s.Chat(ctx, req)
}

func TestTimeout(t *testing.T) {
	_, err := New(slow{}, Timeout(20*time.Millisecond)).Run(context.Background(), ask("x"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}

func TestCancelledDuringToolFailsTurn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stop := func(next ToolFunc) ToolFunc {
		return func(ctx context.Context, c model.ToolCall) (json.RawMessage, error) {
			cancel()
			return nil, ctx.Err()
		}
	}
	m := fake.New(fake.ToolCall("find", findIn{Query: "x"}), fake.Text("never"))
	_, err := New(m, Tools(tools(t)), WrapTool(stop)).Run(ctx, ask("x"))
	if !errors.Is(err, context.Canceled) || m.Remaining() != 1 {
		t.Fatalf("err = %v, remaining = %d", err, m.Remaining())
	}
}

// pool hands out one fake model and counts leases.
type pool struct {
	*fake.Client
	acquired, released int
	fail               error
}

func (p *pool) Acquire(_ context.Context, onWait func()) (model.Model, func(), error) {
	if p.fail != nil {
		return nil, nil, p.fail
	}
	p.acquired++
	if onWait != nil {
		onWait()
	}
	return p.Client, func() { p.released++ }, nil
}

func TestProviderIsHeldForTheWholeTurn(t *testing.T) {
	p := &pool{Client: fake.New(fake.ToolCall("find", findIn{Query: "x"}), fake.Text("ok"))}
	var r recorder
	turn := ask("x")
	turn.Stream = r.stream()
	if _, err := New(p, Tools(tools(t))).Run(context.Background(), turn); err != nil {
		t.Fatal(err)
	}
	if p.acquired != 1 || p.released != 1 || r.waits != 1 {
		t.Fatalf("acquired = %d, released = %d, waits = %d", p.acquired, p.released, r.waits)
	}
	p.fail = model.ErrUnavailable
	if _, err := New(p).Run(context.Background(), ask("x")); !errors.Is(err, model.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestTraceClipsResults(t *testing.T) {
	m := fake.New(fake.ToolCall("find", findIn{Query: strings.Repeat("я", 50)}), fake.Text("ok"))
	res, err := New(m, Tools(tools(t)), TraceLimit(11)).Run(context.Background(), ask("x"))
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Trace.Steps[0].Tools[0].Result; !strings.HasSuffix(got, "…") || len(got) > 14 {
		t.Fatalf("result = %q", got)
	}
}
