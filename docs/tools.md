# tool: инструменты, наборы, права, события

## Определение

```go
type moveIn struct {
    From string `json:"from" jsonschema:"source path"`
    To   string `json:"to" jsonschema:"target path"`
    Over bool   `json:"overwrite,omitempty" jsonschema:"replace an existing file"`
}
type moveOut struct {
    Moved bool `json:"moved"`
}

var Move = tool.MustDefine("move_file", "Move a file inside the workspace",
    func(ctx context.Context, in moveIn) (moveOut, error) { ... },
    tool.Tags("fs", "write"))
```

- `Define[In, Out]` возвращает `(Tool, error)`, а `MustDefine` паникует. Удобно для переменных пакета.
- Схема аргументов выводится из `In`, и это должна быть структура:
  - `json` задаёт имя поля, `jsonschema` — описание для модели;
  - поле без `omitempty` обязательное;
  - лишние поля запрещены.
- `Out` — любой тип, который сериализуется в JSON. Это то, что получит модель.
- `Tags` — метки для политики. На поведение инструмента они не влияют.

Хорошее описание инструмента — половина успеха. Пиши, когда звать инструмент и что он вернёт, а не как он
устроен.

## Набор: Set

```go
base, err  := tool.NewSet(Search, Read)  // или tool.MustSet(...)
admin, err := base.With(Move, Delete)     // новый набор, base не меняется
safe       := admin.Without("delete")
```

Набор неизменяемый, поэтому один набор можно отдать нескольким агентам. Два инструмента с одним именем — ошибка.

| Метод | Что делает |
|---|---|
| `Defs(ctx, policy)` | инструменты, которые разрешены вызывающему в `ctx`, в виде `[]model.ToolDef` для модели |
| `Call(ctx, policy, name, args)` | вызов, как его делает агент: инструмент существует → политика → схема → обработчик → JSON |
| `Names()`, `Get(name)` | что внутри |

`Call` можно использовать и без агента, например чтобы выполнить действие по команде из UI с теми же проверками.

## Вызывающий

```go
ctx = tool.WithCaller(ctx, User{ID: "u1", Role: "admin"}) // агент делает это сам из Turn.Caller
u, ok := tool.CallerFrom[User](ctx)                       // в политике или обработчике
```

Тип вызывающего выбираешь ты: пользователь, задача планировщика, другой агент.

## Политика

```go
type Policy interface {
    Allow(ctx context.Context, t tool.Info) bool // Info: Name, Description, Tags
}
```

Агент спрашивает политику дважды: когда строит список для модели и когда модель вызывает инструмент. Если
политика `nil`, разрешено всё.

| Готовая политика | Правило |
|---|---|
| `AllowAll` | всё |
| `AllowNames("a", "b")` | только перечисленные |
| `AllowTags(func(ctx) []string)` | есть хотя бы одна метка из списка для вызывающего. Инструмент без меток не разрешён |
| `Any(p...)`, `All(p...)` | любая из политик, все политики |
| `PolicyFunc(f)` | своя функция |

```go
byRole := tool.AllowTags(func(ctx context.Context) []string {
    u, _ := tool.CallerFrom[User](ctx)
    if u.Role == "admin" {
        return []string{"fs", "write"}
    }
    return []string{"fs"}
})
policy := tool.Any(byRole, tool.AllowNames("ping")) // ping доступен всем
```

**Где какая проверка.** Политика отвечает на вопрос «можно ли этому вызывающему этот инструмент». Вопрос «можно
ли ему этот объект» (чужой файл, чужая запись) решает обработчик: он возвращает `tool.ErrForbidden`.

## Ошибки

| Ошибка | Код для модели | Когда |
|---|---|---|
| `ErrUnknownTool` | `unknown_tool` | такого инструмента нет в наборе |
| `ErrForbidden` | `forbidden` | отказала политика или обработчик |
| `ErrInvalidArgs` | `invalid_args` | аргументы не по схеме или обработчик вернул её сам |
| `ErrNotFound` | `not_found` | объекта нет для этого вызывающего |
| любая другая | `tool_failed` | сбой обработчика |

Модель получает `{"error": код, "message": текст}` (`tool.ErrorJSON`). Текст ошибки обработчика наружу не
уходит: там могут быть адреса, пути и секреты. Исключение — `invalid_args`: оборачивай её с понятной причиной,
`fmt.Errorf("%w: limit must be 1..10", tool.ErrInvalidArgs)`, и модель исправит аргументы.

## События

Инструмент может сообщить миру что-то помимо результата для модели: показать карточку, подсветить элемент,
отправить уведомление.

```go
tool.Emit(ctx, "notify", Notification{Title: "Файл перемещён"})
```

Агент собирает события в `Result.Events` и сразу отдаёт в `Stream.Event`. Поле `Event.Tool` заполняется само.
Что значит тип события, решает приложение. Без приёмника (`tool.WithSink`) `Emit` ничего не делает, так что
обработчик можно вызывать и вне агента.
