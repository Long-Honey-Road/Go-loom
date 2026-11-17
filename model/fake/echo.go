package fake

import (
	"context"

	"github.com/Long-Honey-Road/Go-loom/model"
)

// Echo answers every request with the same text: for demos and UI work without a model.
type Echo struct{ text string }

func NewEcho(text string) *Echo { return &Echo{text: text} }

func (e *Echo) Chat(ctx context.Context, req model.Request) (model.Response, error) {
	return e.Stream(ctx, req, func(string) {})
}

func (e *Echo) Stream(ctx context.Context, _ model.Request, onDelta func(string)) (model.Response, error) {
	c := New(Text(e.text))
	return c.Stream(ctx, model.Request{}, onDelta)
}
