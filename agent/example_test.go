package agent_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/Long-Honey-Road/Go-loom/agent"
	"github.com/Long-Honey-Road/Go-loom/model"
	"github.com/Long-Honey-Road/Go-loom/model/fake"
	"github.com/Long-Honey-Road/Go-loom/tool"
)

type weatherIn struct {
	City string `json:"city" jsonschema:"city name"`
}

type weatherOut struct {
	TempC int `json:"temp_c"`
}

func Example() {
	weather := tool.MustDefine("weather", "Current temperature in a city",
		func(ctx context.Context, in weatherIn) (weatherOut, error) {
			return weatherOut{TempC: 21}, nil
		})

	// A scripted model: first it calls the tool, then it answers.
	m := fake.New(
		fake.ToolCall("weather", weatherIn{City: "Paris"}),
		fake.Text("In Paris it is 21°C."),
	)

	a := agent.New(m, agent.Tools(tool.MustSet(weather)))
	res, err := a.Run(context.Background(), agent.Turn{
		System:   "You answer about the weather.",
		Messages: []model.Message{{Role: model.RoleUser, Content: "How warm is it in Paris?"}},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(res.Text)
	fmt.Println(res.Trace.Steps[0].Tools[0].Name, res.Trace.Steps[0].Tools[0].Result)
	// Output:
	// In Paris it is 21°C.
	// weather {"temp_c":21}
}

func ExampleCheck() {
	// Send the model back once when it answered without calling any tool.
	grounded := agent.Check(func(_ context.Context, a *agent.Answer) string {
		if a.ToolCalls == 0 {
			return "Call a tool before answering."
		}
		return ""
	})
	// Rewrite the answer: checks run in order and see each other's text.
	shout := agent.Check(func(_ context.Context, a *agent.Answer) string {
		a.Text = strings.ToUpper(a.Text)
		return ""
	})
	m := fake.New(fake.Text("from memory"), fake.Text("still from memory"))
	res, _ := agent.New(m, agent.Checks(grounded, shout)).Run(context.Background(), agent.Turn{
		Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}},
	})
	fmt.Println(res.Text)
	fmt.Println(res.Trace.Notes)
	// Output:
	// STILL FROM MEMORY
	// [retry: Call a tool before answering. retries over, kept: Call a tool before answering.]
}
