package app

// ActiveModel returns the id of the currently active model (e.g.
// "claude-opus-4-8"), or "" if none has been resolved yet. It exists so the CLI
// can report per-session cost without reaching into App's unexported fields.
func (a *App) ActiveModel() string { return a.activeModel }
