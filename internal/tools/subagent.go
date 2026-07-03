package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// DelegateRunner drives a scoped, nested run of the agent loop for a bounded
// sub-task and returns only its final summary text. It is injected by the agent
// package (which owns the Agent type and the provider/confirmFn) so that this
// tool can spawn a nested loop without the tools package importing agent — that
// would create an import cycle, since agent already imports tools.
//
// The runner is responsible for the hard safety guarantees of delegation:
//   - the nested loop must NOT itself be able to delegate (recursion depth ≤ 1),
//   - it must run with a strictly smaller iteration budget than the parent, and
//   - it must reuse the parent's real confirmFn so any Write/Elevated/Destructive
//     sub-action is gated identically to a top-level call.
type DelegateRunner func(ctx context.Context, task string) (string, error)

// Delegate ("delegate" tool) spawns a scoped, nested instance of the agent loop
// to handle a bounded sub-task and returns only its final summary to the parent
// conversation. The intermediate steps of the nested loop are not streamed back
// as separate top-level events; the parent sees a single tool call with a single
// summarized result.
type Delegate struct {
	run DelegateRunner
}

// NewDelegate builds a Delegate backed by the given runner. The runner is
// supplied by the agent package; see DelegateRunner for the invariants it must
// uphold.
func NewDelegate(run DelegateRunner) *Delegate { return &Delegate{run: run} }

func (d *Delegate) Name() string { return "delegate" }

func (d *Delegate) Description() string {
	return "Delegate a bounded sub-task to a scoped nested agent. Provide a self-contained task description; the sub-agent runs its own tool-calling loop (with a smaller iteration budget) and returns only a final summary. Use this to isolate a well-defined chunk of work — the sub-agent cannot itself delegate, and any changes it makes still require the same confirmations as your own tool calls. Do not use it for trivial single steps you can do directly."
}

func (d *Delegate) Schema() map[string]any {
	return objectSchema(
		map[string]any{
			"task": strProp("A self-contained description of the sub-task for the nested agent to accomplish. Include all context it needs; it does not see this conversation's history."),
		},
		"task",
	)
}

// TaskArg extracts the "task" field from a delegate tool call input, for display
// in the confirmation prompt. Returns "" when absent.
func TaskArg(input json.RawMessage) string {
	var args struct {
		Task string `json:"task"`
	}
	_ = json.Unmarshal(input, &args)
	return args.Task
}

func (d *Delegate) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Task string `json:"task"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", errors.New("delegate: invalid input: " + err.Error())
	}
	task := strings.TrimSpace(args.Task)
	if task == "" {
		return "", errors.New("delegate: task must not be empty")
	}
	if d.run == nil {
		// A Delegate with no runner cannot spawn a nested loop. This happens only
		// if the tool is constructed outside the agent package (which never adds
		// it to the nested agent's registry), so treat it as a hard guardrail.
		return "", errors.New("delegate: not available in this context")
	}
	return d.run(ctx, task)
}
