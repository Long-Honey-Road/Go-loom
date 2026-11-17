package tool

import "context"

type (
	callerKey struct{}
	toolKey   struct{}
	sinkKey   struct{}
)

// WithCaller puts the caller (a user, a scheduler job, any type you choose) into ctx for policies and handlers.
func WithCaller(ctx context.Context, caller any) context.Context {
	return context.WithValue(ctx, callerKey{}, caller)
}

// CallerFrom returns the caller in ctx if it has type T.
func CallerFrom[T any](ctx context.Context) (T, bool) {
	c, ok := ctx.Value(callerKey{}).(T)
	return c, ok
}

// Event is something a tool wants the outside world to know or do, apart from the result it gives the model:
// show a card, highlight an item, send a notification. The agent collects events and hands them to the
// application; what they mean is up to the application.
type Event struct {
	// Tool is the tool that sent the event; Set.Call fills it.
	Tool string `json:"tool"`
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

// Emit sends an event from a handler. Without a sink in ctx (WithSink) it does nothing.
func Emit(ctx context.Context, typ string, data any) {
	if sink, ok := ctx.Value(sinkKey{}).(func(Event)); ok {
		name, _ := ctx.Value(toolKey{}).(string)
		sink(Event{Tool: name, Type: typ, Data: data})
	}
}

// WithSink makes Emit in ctx deliver events to sink. The agent sets it for every turn.
func WithSink(ctx context.Context, sink func(Event)) context.Context {
	return context.WithValue(ctx, sinkKey{}, sink)
}

func withTool(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, toolKey{}, name)
}
