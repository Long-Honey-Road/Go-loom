// Command chat is a terminal chat with one tool against any OpenAI-compatible API.
//
//	LOOM_BASE_URL=http://localhost:11434/v1 LOOM_MODEL=qwen3.6:35b-a3b go run ./examples/chat
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Long-Honey-Road/Go-loom/agent"
	"github.com/Long-Honey-Road/Go-loom/model"
	"github.com/Long-Honey-Road/Go-loom/model/openai"
	"github.com/Long-Honey-Road/Go-loom/tool"
)

type clockIn struct {
	Zone string `json:"zone,omitempty" jsonschema:"IANA time zone, e.g. Europe/Moscow; empty for local"`
}

type clockOut struct {
	Time string `json:"time"`
}

var clock = tool.MustDefine("clock", "Current date and time", func(_ context.Context, in clockIn) (clockOut, error) {
	loc := time.Local
	if in.Zone != "" {
		l, err := time.LoadLocation(in.Zone)
		if err != nil {
			return clockOut{}, fmt.Errorf("%w: unknown zone %q", tool.ErrInvalidArgs, in.Zone)
		}
		loc = l
	}
	return clockOut{Time: time.Now().In(loc).Format(time.RFC1123)}, nil
})

func main() {
	m := openai.New(openai.Config{
		BaseURL:         env("LOOM_BASE_URL", "http://localhost:11434/v1"),
		Model:           env("LOOM_MODEL", "qwen3.6:35b-a3b"),
		APIKey:          env("LOOM_API_KEY", "ollama"),
		ReasoningEffort: os.Getenv("LOOM_REASONING_EFFORT"),
	})
	a := agent.New(m, agent.Tools(tool.MustSet(clock)))

	var history []model.Message
	in := bufio.NewScanner(os.Stdin)
	for fmt.Print("> "); in.Scan(); fmt.Print("> ") {
		history = append(history, model.Message{Role: model.RoleUser, Content: in.Text()})
		res, err := a.Run(context.Background(), agent.Turn{
			System:   "You are a concise assistant. Use tools for facts you cannot know.",
			Messages: history,
			Stream: agent.Stream{
				Delta:    func(d string) { fmt.Print(d) },
				Reset:    func() { fmt.Println() },
				ToolCall: func(c model.ToolCall) { fmt.Printf("[%s %s]\n", c.Name, c.Arguments) },
			},
		})
		fmt.Println()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			history = history[:len(history)-1]
			continue
		}
		history = append(history, res.Messages...)
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
