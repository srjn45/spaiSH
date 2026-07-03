package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"spaish/internal/config"
)

// makeSettingsFile writes a .spai/settings.toml under dir.
func makeSettingsFile(t *testing.T, dir, content string) {
	t.Helper()
	spaiDir := filepath.Join(dir, ".spai")
	if err := os.MkdirAll(spaiDir, 0755); err != nil {
		t.Fatalf("mkdir .spai: %v", err)
	}
	if err := os.WriteFile(filepath.Join(spaiDir, "settings.toml"), []byte(content), 0644); err != nil {
		t.Fatalf("write settings.toml: %v", err)
	}
}

const sampleSettings = `
[permissions]
allow_commands = ["go test ./..."]

[permissions.tools]
write_file = "allow"
read_file  = "deny"

[permissions.mcp_servers]
fs = "allow"
`

func TestMergeProjectPermissions_FoundInCwd(t *testing.T) {
	dir := t.TempDir()
	makeSettingsFile(t, dir, sampleSettings)

	tools, mcp, cmds := config.MergeProjectPermissions(nil, nil, nil, dir)

	if tools["write_file"] != "allow" {
		t.Errorf("tools.write_file = %q, want allow", tools["write_file"])
	}
	if tools["read_file"] != "deny" {
		t.Errorf("tools.read_file = %q, want deny", tools["read_file"])
	}
	if mcp["fs"] != "allow" {
		t.Errorf("mcp.fs = %q, want allow", mcp["fs"])
	}
	if len(cmds) != 1 || cmds[0] != "go test ./..." {
		t.Errorf("cmds = %v, want [go test ./...]", cmds)
	}
}

func TestMergeProjectPermissions_FoundInAncestor(t *testing.T) {
	root := t.TempDir()
	makeSettingsFile(t, root, sampleSettings)

	// Create a nested subdirectory three levels deep.
	child := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(child, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	tools, _, _ := config.MergeProjectPermissions(nil, nil, nil, child)
	if tools["write_file"] != "allow" {
		t.Errorf("expected project settings discovered from ancestor; tools.write_file = %q", tools["write_file"])
	}
}

func TestMergeProjectPermissions_NotFound(t *testing.T) {
	dir := t.TempDir()
	// No .spai/settings.toml present — global values pass through unchanged.
	globalTools := map[string]string{"bash": "confirm"}
	tools, mcp, cmds := config.MergeProjectPermissions(globalTools, nil, []string{"ls"}, dir)

	if tools["bash"] != "confirm" {
		t.Errorf("global tools should be preserved; got %v", tools)
	}
	if mcp != nil {
		t.Errorf("mcp should be nil; got %v", mcp)
	}
	if len(cmds) != 1 || cmds[0] != "ls" {
		t.Errorf("cmds should be [ls]; got %v", cmds)
	}
}

func TestMergeProjectPermissions_GitBoundary(t *testing.T) {
	// Place settings above the .git root — should NOT be discovered.
	root := t.TempDir()
	makeSettingsFile(t, root, sampleSettings)

	// Create a subdirectory with a .git marker — this is the git root.
	gitRoot := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(gitRoot, ".git"), 0755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	// Working dir is inside the git root; settings are above it.
	workdir := filepath.Join(gitRoot, "src")
	if err := os.MkdirAll(workdir, 0755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}

	tools, _, _ := config.MergeProjectPermissions(nil, nil, nil, workdir)
	if len(tools) != 0 {
		t.Errorf("settings above git root should not be loaded; got tools=%v", tools)
	}
}

func TestMergeProjectPermissions_TOMLParsing(t *testing.T) {
	dir := t.TempDir()
	makeSettingsFile(t, dir, `
[permissions]
allow_commands = ["git status", "go build"]

[permissions.tools]
bash = "deny"

[permissions.mcp_servers]
dangerous = "deny"
safe       = "allow"
`)

	tools, mcp, cmds := config.MergeProjectPermissions(nil, nil, nil, dir)

	if tools["bash"] != "deny" {
		t.Errorf("tools.bash = %q, want deny", tools["bash"])
	}
	if mcp["dangerous"] != "deny" {
		t.Errorf("mcp.dangerous = %q, want deny", mcp["dangerous"])
	}
	if mcp["safe"] != "allow" {
		t.Errorf("mcp.safe = %q, want allow", mcp["safe"])
	}
	if len(cmds) != 2 {
		t.Errorf("cmds length = %d, want 2", len(cmds))
	}
}

func TestMergeProjectPermissions_ProjectOverridesGlobal(t *testing.T) {
	dir := t.TempDir()
	makeSettingsFile(t, dir, `
[permissions.tools]
bash       = "allow"
write_file = "allow"
`)

	globalTools := map[string]string{
		"bash":      "deny",   // project should override to "allow"
		"read_file": "allow",  // project has no entry — global survives
	}
	globalCmds := []string{"ls"}

	tools, _, cmds := config.MergeProjectPermissions(globalTools, nil, globalCmds, dir)

	if tools["bash"] != "allow" {
		t.Errorf("project should override global; bash = %q, want allow", tools["bash"])
	}
	if tools["read_file"] != "allow" {
		t.Errorf("global-only key should survive; read_file = %q, want allow", tools["read_file"])
	}
	if tools["write_file"] != "allow" {
		t.Errorf("project-only key should appear; write_file = %q, want allow", tools["write_file"])
	}
	if len(cmds) != 1 || cmds[0] != "ls" {
		t.Errorf("cmds should be global [ls] when project has no allow_commands; got %v", cmds)
	}
}

func TestMergeProjectPermissions_AllowCommandsUnion(t *testing.T) {
	dir := t.TempDir()
	makeSettingsFile(t, dir, `
[permissions]
allow_commands = ["go test", "make"]
`)

	_, _, cmds := config.MergeProjectPermissions(nil, nil, []string{"ls", "git status"}, dir)

	if len(cmds) != 4 {
		t.Fatalf("expected 4 allow_commands (global + project), got %d: %v", len(cmds), cmds)
	}
	// Global commands come first.
	if cmds[0] != "ls" || cmds[1] != "git status" {
		t.Errorf("global commands should come first; got %v", cmds)
	}
	if cmds[2] != "go test" || cmds[3] != "make" {
		t.Errorf("project commands should be appended; got %v", cmds)
	}
}
