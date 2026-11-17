# mcp: инструменты наружу

`mcp.NewServer` отдаёт `tool.Set` по Model Context Protocol. Любой MCP-клиент (Claude Code, MCP Inspector, другой
агент) получает те же инструменты с той же политикой, что и твой агент.

```go
srv := mcp.NewServer(set, policy, User{ID: "u1", Role: "user"}, mcp.Options{
    Name:         "my-daemon",
    Version:      "0.1.0",
    Instructions: "Tools of my daemon, acting as one user.",
    OnEvent:      func(e tool.Event) { log.Println(e) }, // nil — события выбрасываются
})

// stdio: демон как MCP-сервер для локального клиента
err := srv.Run(ctx, &sdk.StdioTransport{})
```

Для HTTP оберни сервер в `sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return srv }, nil)`
из `github.com/modelcontextprotocol/go-sdk/mcp`. Если вызывающие разные, собирай сервер на каждого: вызывающий
задаётся при создании.

Поведение:
- `tools/list` показывает только то, что политика разрешила вызывающему.
- `tools/call` идёт через `Set.Call` с теми же проверками. Вызов неразрешённого инструмента по имени получает
  отказ набора (`forbidden`), а не «unknown tool» от SDK.
- Ошибка приходит как `{"error": код, "message": текст}` с `isError: true`, успех — текстом и в
  `structuredContent`.

Обратного направления (подключить чужой MCP-сервер как инструменты агента) пока нет. Это естественный способ
делать плагины на любом языке, когда понадобится.
