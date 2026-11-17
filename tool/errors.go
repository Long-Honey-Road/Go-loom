package tool

import (
	"encoding/json"
	"errors"
	"fmt"
)

var (
	// ErrUnknownTool: the model called a tool that is not in the set.
	ErrUnknownTool = errors.New("unknown tool")
	// ErrForbidden: the policy or the handler refused the caller. Return it from a handler for object checks.
	ErrForbidden = errors.New("forbidden")
	// ErrInvalidArgs: arguments do not match the schema. Return it from a handler for checks the schema cannot
	// express; its text goes to the model, so say what to fix.
	ErrInvalidArgs = errors.New("invalid tool arguments")
	// ErrNotFound: the object the tool was asked about does not exist for this caller.
	ErrNotFound = errors.New("not found")
)

// Error codes a model or an MCP client sees.
const (
	CodeUnknownTool = "unknown_tool"
	CodeForbidden   = "forbidden"
	CodeInvalidArgs = "invalid_args"
	CodeNotFound    = "not_found"
	CodeFailed      = "tool_failed"
)

// Code maps an error of Set.Call to its code; anything else is CodeFailed.
func Code(err error) string {
	switch {
	case errors.Is(err, ErrUnknownTool):
		return CodeUnknownTool
	case errors.Is(err, ErrForbidden):
		return CodeForbidden
	case errors.Is(err, ErrInvalidArgs):
		return CodeInvalidArgs
	case errors.Is(err, ErrNotFound):
		return CodeNotFound
	default:
		return CodeFailed
	}
}

// Explain gives the code and a message for the model. The handler's own error text never reaches the model,
// except for invalid arguments, where it tells the model what to fix.
func Explain(err error, name string) (code, message string) {
	code = Code(err)
	switch code {
	case CodeUnknownTool:
		return code, fmt.Sprintf("there is no tool named %s", name)
	case CodeForbidden:
		return code, "this action or data is not available to the user"
	case CodeInvalidArgs:
		return code, err.Error()
	case CodeNotFound:
		return code, fmt.Sprintf("%s found nothing with these arguments", name)
	default:
		return code, "the tool failed; answer without it or try again later"
	}
}

// ErrorJSON is Explain as the JSON a model gets instead of a result: {"error": code, "message": text}.
func ErrorJSON(err error, name string) json.RawMessage {
	code, msg := Explain(err, name)
	b, _ := json.Marshal(map[string]string{"error": code, "message": msg})
	return b
}
