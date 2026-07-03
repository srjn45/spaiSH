package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestTodoWrite_Run(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name    string
		input   string
		wantOut string
		wantErr bool
	}{
		{
			name:    "empty list",
			input:   `{"tasks":[]}`,
			wantOut: "Tasks: (none)\n",
		},
		{
			name:    "creates a list with all statuses",
			input:   `{"tasks":[{"description":"Write tests","status":"completed"},{"description":"Implement tool","status":"in_progress"},{"description":"Open PR","status":"pending"}]}`,
			wantOut: "Tasks:\n  [x] Write tests\n  [~] Implement tool\n  [ ] Open PR\n",
		},
		{
			name:    "single pending task",
			input:   `{"tasks":[{"description":"Step 1","status":"pending"}]}`,
			wantOut: "Tasks:\n  [ ] Step 1\n",
		},
		{
			name:    "marks all complete",
			input:   `{"tasks":[{"description":"Task A","status":"completed"},{"description":"Task B","status":"completed"}]}`,
			wantOut: "Tasks:\n  [x] Task A\n  [x] Task B\n",
		},
		{
			name:    "in_progress status",
			input:   `{"tasks":[{"description":"Running now","status":"in_progress"}]}`,
			wantOut: "Tasks:\n  [~] Running now\n",
		},
		{
			name:    "malformed JSON",
			input:   `{not valid json`,
			wantErr: true,
		},
		{
			name:    "invalid status value",
			input:   `{"tasks":[{"description":"Do thing","status":"done"}]}`,
			wantErr: true,
		},
		{
			name:    "invalid status second item",
			input:   `{"tasks":[{"description":"A","status":"pending"},{"description":"B","status":"unknown"}]}`,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tool := &TodoWrite{}
			out, err := tool.Run(ctx, json.RawMessage(tc.input))
			if tc.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out != tc.wantOut {
				t.Errorf("output mismatch\ngot:  %q\nwant: %q", out, tc.wantOut)
			}
		})
	}
}

// TestTodoWrite_Replace verifies that each call replaces (not appends to) the
// task list, and that Tasks() reflects the latest state.
func TestTodoWrite_Replace(t *testing.T) {
	ctx := context.Background()
	tool := &TodoWrite{}

	_, err := tool.Run(ctx, json.RawMessage(`{"tasks":[{"description":"Old task","status":"pending"}]}`))
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if got := tool.Tasks(); len(got) != 1 || got[0].Description != "Old task" {
		t.Fatalf("after first call: unexpected tasks %v", got)
	}

	out, err := tool.Run(ctx, json.RawMessage(`{"tasks":[{"description":"New A","status":"completed"},{"description":"New B","status":"in_progress"}]}`))
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	got := tool.Tasks()
	if len(got) != 2 {
		t.Fatalf("expected 2 tasks after replace, got %d: %v", len(got), got)
	}
	if got[0].Description != "New A" || got[0].Status != "completed" {
		t.Errorf("task[0] wrong: %+v", got[0])
	}
	if got[1].Description != "New B" || got[1].Status != "in_progress" {
		t.Errorf("task[1] wrong: %+v", got[1])
	}
	if !strings.Contains(out, "[x] New A") || !strings.Contains(out, "[~] New B") {
		t.Errorf("output missing expected markers: %q", out)
	}
}

// TestTodoWrite_UpdateStatuses verifies a common workflow: create a list, then
// update statuses on a subsequent call.
func TestTodoWrite_UpdateStatuses(t *testing.T) {
	ctx := context.Background()
	tool := &TodoWrite{}

	_, _ = tool.Run(ctx, json.RawMessage(`{"tasks":[{"description":"Step 1","status":"pending"},{"description":"Step 2","status":"pending"}]}`))

	out, err := tool.Run(ctx, json.RawMessage(`{"tasks":[{"description":"Step 1","status":"completed"},{"description":"Step 2","status":"in_progress"}]}`))
	if err != nil {
		t.Fatalf("update call: %v", err)
	}
	want := "Tasks:\n  [x] Step 1\n  [~] Step 2\n"
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

func TestTodoWrite_Interface(t *testing.T) {
	var _ Tool = &TodoWrite{}
}

func TestRenderTaskList(t *testing.T) {
	tasks := []Task{
		{Description: "Alpha", Status: "completed"},
		{Description: "Beta", Status: "in_progress"},
		{Description: "Gamma", Status: "pending"},
	}
	got := renderTaskList(tasks)
	want := "Tasks:\n  [x] Alpha\n  [~] Beta\n  [ ] Gamma\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderTaskList_Empty(t *testing.T) {
	got := renderTaskList(nil)
	if got != "Tasks: (none)\n" {
		t.Errorf("got %q", got)
	}
}
