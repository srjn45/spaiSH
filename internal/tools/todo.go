package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// Task is a single item in the agent's in-session task list.
type Task struct {
	Description string `json:"description"`
	Status      string `json:"status"` // "pending" | "in_progress" | "completed"
}

// TodoWrite replaces the agent's in-memory task list for the current session.
// State is scoped to one Agent.Run() call; there is no cross-session persistence.
type TodoWrite struct {
	mu    sync.Mutex
	tasks []Task
}

func (t *TodoWrite) Name() string { return "todo_write" }
func (t *TodoWrite) Description() string {
	return "Write the full task list for this session, replacing the current list. Use this to track multi-step progress: create the list at the start, update statuses as you go, and mark items completed when done. Each call replaces the entire list — always include all tasks, not just changed ones."
}

func (t *TodoWrite) Schema() map[string]any {
	return objectSchema(
		map[string]any{
			"tasks": map[string]any{
				"type":        "array",
				"description": "Complete task list, replacing the current in-session list.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"description": strProp("Short description of the task."),
						"status": map[string]any{
							"type":        "string",
							"enum":        []string{"pending", "in_progress", "completed"},
							"description": `Task status: "pending" (not started), "in_progress" (active), "completed" (done).`,
						},
					},
					"required": []string{"description", "status"},
				},
			},
		},
		"tasks",
	)
}

func (t *TodoWrite) Run(_ context.Context, input json.RawMessage) (string, error) {
	var args struct {
		Tasks []Task `json:"tasks"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("todo_write: invalid input: %w", err)
	}
	for i, task := range args.Tasks {
		switch task.Status {
		case "pending", "in_progress", "completed":
		default:
			return "", fmt.Errorf("todo_write: task %d has invalid status %q (must be pending, in_progress, or completed)", i, task.Status)
		}
	}
	t.mu.Lock()
	t.tasks = make([]Task, len(args.Tasks))
	copy(t.tasks, args.Tasks)
	t.mu.Unlock()
	return renderTaskList(args.Tasks), nil
}

// Tasks returns a snapshot of the current task list.
func (t *TodoWrite) Tasks() []Task {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Task, len(t.tasks))
	copy(out, t.tasks)
	return out
}

// renderTaskList formats the task slice as a plain-text checklist. This output
// is both returned to the model as a tool result and displayed to the user via
// the "todo" response type in the renderer.
func renderTaskList(tasks []Task) string {
	if len(tasks) == 0 {
		return "Tasks: (none)\n"
	}
	var sb strings.Builder
	sb.WriteString("Tasks:\n")
	for _, task := range tasks {
		var mark string
		switch task.Status {
		case "completed":
			mark = "[x]"
		case "in_progress":
			mark = "[~]"
		default:
			mark = "[ ]"
		}
		fmt.Fprintf(&sb, "  %s %s\n", mark, task.Description)
	}
	return sb.String()
}
