package tools

import "context"

// Checkpointer records the pre-mutation state of files. A mutating tool calls
// Snapshot(paths...) once, before it writes, passing every path it will touch,
// so the change can later be undone. The interface is deliberately narrow: it
// lives here (not in session) so the mutating tools can depend on it without
// importing session, and session supplies the concrete implementation
// (dependency inversion).
type Checkpointer interface {
	Snapshot(paths ...string) error
}

// checkpointerKey is the context key under which a Checkpointer is threaded from
// the agent into the tool dispatch.
type checkpointerKey struct{}

// WithCheckpointer returns a copy of ctx carrying c, so mutating tools invoked
// with that ctx snapshot files before writing.
func WithCheckpointer(ctx context.Context, c Checkpointer) context.Context {
	return context.WithValue(ctx, checkpointerKey{}, c)
}

// checkpointerFrom returns the Checkpointer carried by ctx, or nil when none is
// present (one-shot runs, unit tests, plan mode) — in which case snapshotting is
// a no-op.
func checkpointerFrom(ctx context.Context) Checkpointer {
	c, _ := ctx.Value(checkpointerKey{}).(Checkpointer)
	return c
}

// snapshot is the one-liner mutating tools call before writing: it records the
// pre-mutation bytes of paths when a checkpointer is installed, and does nothing
// otherwise. Snapshot errors are best-effort and never block the write.
func snapshot(ctx context.Context, paths ...string) {
	if c := checkpointerFrom(ctx); c != nil {
		_ = c.Snapshot(paths...)
	}
}
