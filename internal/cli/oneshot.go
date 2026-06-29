package cli

import (
	"context"

	"spaish/internal/app"
	"spaish/internal/protocol"
)

// RunOneShot executes a single agent request, rendering the streamed response
// with a working spinner (until the first output) and interactive confirmation
// for tier-gated tool calls.
func RunOneShot(ctx context.Context, a *app.App, req *protocol.Request) error {
	sp := NewSpinner("thinking")
	sp.Start()
	stopped := false
	stopSpinner := func() {
		if !stopped {
			sp.Stop()
			stopped = true
		}
	}

	confirm := func(creq protocol.ConfirmRequest) bool {
		stopSpinner()
		return PromptConfirm(creq)
	}

	rd := NewRenderer()
	err := a.RunAgent(ctx, req, confirm, func(resp protocol.Response) {
		stopSpinner()
		rd.Render(resp)
	})
	stopSpinner()
	rd.Flush()
	return err
}
