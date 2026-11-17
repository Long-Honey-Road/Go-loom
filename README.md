# loom

Ядро для ИИ-агентов на Go: модель, типизированные инструменты, политика прав и цикл хода с лимитами,
стримингом, хуками и трассой.
## Установка

```bash
go get github.com/Long-Honey-Road/Go-loom@latest
```

Нужен Go 1.26+. Пока репозиторий приватный, Go должен знать, что модуль не надо искать в публичном прокси:

```bash
go env -w GOPRIVATE=github.com/Long-Honey-Road
```

Версии — git-теги `vX.Y.Z`: `go get github.com/Long-Honey-Road/Go-loom@v0.1.0`. Чтобы править loom и проект вместе,
подключи локальную копию в `go.mod` проекта: `replace github.com/Long-Honey-Road/Go-loom => ../loom`.

## Минимальный агент

```go
type weatherIn struct {
    City string `json:"city" jsonschema:"city name"`
}
type weatherOut struct {
    TempC int `json:"temp_c"`
}

weather := tool.MustDefine("weather", "Current temperature in a city",
    func(ctx context.Context, in weatherIn) (weatherOut, error) {
        return weatherOut{TempC: 21}, nil
    })

m := openai.New(openai.Config{BaseURL: "http://localhost:11434/v1", Model: "qwen3.6:35b-a3b", APIKey: "ollama"})
a := agent.New(m, agent.Tools(tool.MustSet(weather)))

res, err := a.Run(ctx, agent.Turn{
    System:   "You answer about the weather.",
    Messages: []model.Message{{Role: model.RoleUser, Content: "How warm is it in Paris?"}},
    Stream:   agent.Stream{Delta: func(d string) { fmt.Print(d) }},
})
```

Живой пример с историей и стримингом в терминал — [`examples/chat`](examples/chat/main.go).

## Пакеты

| Пакет | Что внутри | Документация |
|---|---|---|
| `model` | контракт с моделью: сообщения, запрос, ответ, `Model`, `Provider` (пул) | [docs/model.md](docs/model.md) |
| `model/openai` | `Model` для любого OpenAI-совместимого API: OpenAI, Ollama, vLLM, OpenRouter | [docs/model.md](docs/model.md#openai) |
| `model/fake` | модель по сценарию для тестов | [docs/testing.md](docs/testing.md) |
| `tool` | `Define`, наборы `Set`, политика `Policy`, ошибки, вызывающий в контексте, события | [docs/tools.md](docs/tools.md) |
| `agent` | цикл хода: опции, `Turn`, `Stream`, `Result`, хуки, проверки, трасса | [docs/agent.md](docs/agent.md) |
| `mcp` | набор инструментов наружу по MCP с той же политикой | [docs/mcp.md](docs/mcp.md) |

Зависимости: `model`, `tool` и `agent` тянут только `google/jsonschema-go`. `openai-go` и MCP SDK подключаются,
только если импортировать `model/openai` и `mcp`.

## Как устроен ход

```
Turn ─▶ [Provider.Acquire] ─▶ таймаут ─▶ ┌─ модель (стрим) ─────────────────────────────┐
                                         │  вызвала инструменты?                        │
                                         │   да ─▶ политика ─▶ схема ─▶ обработчик ─┐   │
                                         │         ошибки → {"error","message"}     │   │
                                         │   ◀──────── результат в историю ─────────┘   │
                                         │   нет ─▶ проверки: правка текста / повтор    │
                                         └─────────────── лимит шагов: без инструментов ┘
                                                              ─▶ Result: текст, сообщения, события, трасса
```

## Принципы

- **Модель видит только разрешённое.** Политика решает, какие инструменты показать, и проверяет ещё раз при
  вызове. Скрытый инструмент нельзя вызвать по имени.
- **Аргументы проверяются до обработчика** по схеме, выведенной из Go-структуры. Лишние поля — ошибка.
- **Ошибки инструментов не роняют ход.** Модель получает код ошибки и идёт дальше. Текст ошибки обработчика
  модели не уходит, кроме `invalid_args`: по нему модель исправляет аргументы.
- **Ход всегда заканчивается ответом.** После лимита шагов последний вызов идёт без инструментов.
- **Всё специфичное для приложения — снаружи ядра:** политика, хуки, проверки и события инструментов.

## Тесты

```bash
go test -race -cover ./...
```
