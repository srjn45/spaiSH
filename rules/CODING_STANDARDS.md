# Coding Standards & Guidelines

These standards apply to all development work in spaiOS, regardless of which AI assistant you use (Claude, Copilot, or others).

## Commits: Small & Meaningful

- **One responsibility per commit** — each commit should represent a single, logical change
- **Atomic commits** — commits must be self-contained and not break functionality
- **Clear commit messages** — use present tense: "Add feature" not "Added feature"
- Format: `type(scope): description`
  - `feat(module): description` — new feature
  - `fix(module): description` — bug fix
  - `refactor(module): description` — code reorganization
  - `docs(module): description` — documentation updates
  - `test(module): description` — test additions/changes
  - `chore(module): description` — maintenance tasks
- **Maximum scope** — if a change requires explaining "and also", it's too broad. Break it up.

## Testing & Review Requirements

- **Write tests first or alongside** — no meaningful code without tests
- **Test before committing** — always verify code passes existing tests and new tests
- **Test coverage** — aim for meaningful coverage, focus on critical paths and edge cases
- **Code review mindset** — before committing, ask:
  - Would another developer understand this code?
  - Are there obvious bugs or edge cases?
  - Can this be simplified?
  - Does it follow project conventions?
- **Run linters/formatters** — code must pass all project quality checks
- **Document complex logic** — include comments explaining the "why" not just the "what"

## High Code Standards

### Readability

- **Clear variable names** — `user_email` not `ue` or `email_param_1`
- **Functions do one thing** — break down complex logic into smaller, named functions
- **Keep functions small** — aim for 20-40 lines, max 100 lines
- **DRY principle** — eliminate duplication, extract common patterns
- **Consistent style** — follow existing code patterns in the module

### Quality Practices

- **No magic numbers** — use named constants with clear intent
- **Handle errors explicitly** — don't silently fail, log or propagate appropriately
- **Type safety** — use types/annotations where supported by the language
- **Avoid premature optimization** — optimize only after profiling, with clear justification
- **No commented-out code** — delete it; version control preserves history

### Code Organization

- **Logical file structure** — related code lives together, separated by concerns
- **Import organization** — group by: standard lib → external → internal, alphabetically
- **Consistent indentation** — 2 or 4 spaces (match project standard), no tabs
- **Max line length** — aim for 88-100 characters, break longer lines clearly

## Go-Specific Standards

### Naming Conventions

- **Package names** — lowercase, concise, single word when possible (e.g., `auth`, `handlers`, `models`)
- **Exported symbols** — PascalCase (e.g., `User`, `GetConfig`, `HTTPServer`)
- **Unexported symbols** — camelCase (e.g., `userCache`, `parseJSON`)
- **Interfaces** — end with `-er` suffix when appropriate (e.g., `Reader`, `Writer`, `Handler`)
- **Constants** — PascalCase for exported, camelCase for unexported (e.g., `MaxRetries`, `defaultTimeout`)

### Error Handling

- **Return errors explicitly** — never ignore errors, handle or propagate them
- **Wrap errors** — use `fmt.Errorf("%w", err)` to maintain error chain
- **Check errors immediately** — don't defer error checking
- **No panic for expected errors** — use panic only for truly exceptional conditions (programmer errors)
- **Meaningful error messages** — include context about what failed and why
- **Sentinel errors** — define and use package-level error variables for specific conditions (e.g., `var ErrNotFound = errors.New("not found")`)

Example:
```go
if err != nil {
    return fmt.Errorf("failed to read config: %w", err)
}
```

### Package Structure

- **One responsibility per package** — avoid dumping everything in a `util` or `common` package
- **Internal packages** — use `internal/` for code not meant to be imported externally
- **Clear exports** — only export what's necessary; keep most functions and types unexported
- **Init functions** — minimize package-level side effects; prefer explicit initialization

### Testing Conventions

- **Table-driven tests** — use test tables for multiple scenarios
- **Test naming** — `TestFunctionName_scenario` or `TestFunctionName_expectedBehavior`
- **Testable code** — pass dependencies (e.g., loggers, DB connections) as parameters, not globals
- **Test files** — keep in same package with `_test` suffix (e.g., `handler_test.go`)
- **Benchmarks** — include benchmarks for performance-critical code (`BenchmarkFunctionName`)
- **100% test coverage** — aim for high coverage, especially for business logic

Example:
```go
func TestParseUser(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    *User
        wantErr bool
    }{
        {"valid", `{"id":1}`, &User{ID: 1}, false},
        {"invalid", `invalid`, nil, true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // test here
        })
    }
}
```

### Formatting & Tools

- **Run `go fmt`** — always format code with `go fmt ./...` before committing
- **Run `go vet`** — check for suspicious constructs with `go vet ./...`
- **Linters** — use `golangci-lint` or equivalent and fix all warnings
- **Imports** — use `goimports` to organize imports, don't manually manage them

### Concurrency

- **Goroutines** — document goroutine lifecycle and cleanup; avoid leaking goroutines
- **Channels** — close channels from sender only; document channel semantics (e.g., "send-only after init")
- **Context** — always pass `context.Context` as first parameter for cancelable operations
- **Mutexes** — protect shared state; avoid deadlocks by consistent lock ordering
- **Avoid global state** — pass state through function parameters and context

### Dependencies

- **Minimal dependencies** — keep `go.mod` lean; question every new dependency
- **Version pinning** — keep dependencies up-to-date; run `go mod tidy` regularly
- **Avoid vendoring** — let `go.mod` manage versions unless team consensus otherwise
- **Internal vs external** — prefer standard library; use external packages only when necessary

### Documentation

- **Package comments** — every package should have a doc comment explaining its purpose
- **Exported symbols** — all exported functions, types, and constants need doc comments
- **Comment style** — start with symbol name; use complete sentences ending with period
- **Examples** — include example functions in tests (`ExampleFunctionName`)

Example:
```go
// Package auth provides authentication and authorization utilities.
package auth

// User represents a system user with credentials and permissions.
type User struct {
    ID       int
    Email    string
    Password string // hashed
}

// Authenticate validates user credentials against the database.
// Returns ErrInvalidCredentials if authentication fails.
func Authenticate(ctx context.Context, email, password string) (*User, error) {
    // implementation
}
```

### Common Gotchas

- **Defer in loops** — deferred functions don't run until function exit; call defer inside loops carefully
- **Slice nil vs empty** — `nil` and empty slices behave differently; be explicit
- **Interface nil checks** — `if err != nil` not `if err != interface{}(nil)`
- **Goroutine panics** — panics in goroutines crash the process; recover with proper error handling
- **Copy on assignment** — structs are copied; use pointers for large structures or when mutation is intended

## Workflow

1. **Understand first** — read existing code, understand patterns, check conventions
2. **Plan changes** — sketch out the approach before coding
3. **Implement incrementally** — small, testable pieces
4. **Test thoroughly** — unit tests, integration tests where appropriate
5. **Self-review** — check code against standards before committing
6. **Commit atomically** — one logical change per commit with clear message

## When Uncertain

- Prefer readability over cleverness
- Prefer explicit over implicit
- Prefer consistency over personal preference
- Ask clarifying questions rather than guessing intent
