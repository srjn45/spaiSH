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
		"bash":      "deny",  // project should override to "allow"
		"read_file": "allow", // project has no entry — global survives
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

// makeCommandFile writes a .spai/commands/<name> file under dir.
func makeCommandFile(t *testing.T, dir, name, content string) {
	t.Helper()
	cmdDir := filepath.Join(dir, ".spai", "commands")
	if err := os.MkdirAll(cmdDir, 0755); err != nil {
		t.Fatalf("mkdir commands: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cmdDir, name), []byte(content), 0644); err != nil {
		t.Fatalf("write command: %v", err)
	}
}

// findCommand returns the discovered command with the given name, or nil.
func findCommand(cmds []config.Command, name string) *config.Command {
	for i := range cmds {
		if cmds[i].Name == name {
			return &cmds[i]
		}
	}
	return nil
}

func TestDiscoverCommands_BasicAndFiltering(t *testing.T) {
	dir := t.TempDir()
	makeCommandFile(t, dir, "Deploy.md", "deploy the app") // stem lower-cased
	makeCommandFile(t, dir, "fix.md", "fix issue #$1")
	makeCommandFile(t, dir, "notes.txt", "ignored, not .md") // non-.md ignored
	makeCommandFile(t, dir, "README", "ignored, no ext")

	cmds, err := config.DiscoverCommands(dir)
	if err != nil {
		t.Fatalf("DiscoverCommands: %v", err)
	}
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d: %+v", len(cmds), cmds)
	}
	if c := findCommand(cmds, "deploy"); c == nil || c.Template != "deploy the app" {
		t.Errorf("deploy command not discovered correctly: %+v", c)
	}
	if findCommand(cmds, "fix") == nil {
		t.Errorf("fix command not discovered")
	}
	if findCommand(cmds, "notes") != nil || findCommand(cmds, "readme") != nil {
		t.Errorf("non-.md files should be ignored")
	}
}

func TestDiscoverCommands_MissingDirIsNoOp(t *testing.T) {
	dir := t.TempDir()
	cmds, err := config.DiscoverCommands(dir)
	if err != nil {
		t.Fatalf("DiscoverCommands: %v", err)
	}
	if len(cmds) != 0 {
		t.Errorf("expected no commands for a project without .spai/commands, got %v", cmds)
	}
}

func TestDiscoverCommands_NearerDirWins(t *testing.T) {
	root := t.TempDir()
	// Mark the git root so the walk is bounded there.
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	makeCommandFile(t, root, "review.md", "root review")

	child := filepath.Join(root, "sub")
	if err := os.MkdirAll(child, 0755); err != nil {
		t.Fatal(err)
	}
	makeCommandFile(t, child, "review.md", "child review")

	cmds, err := config.DiscoverCommands(child)
	if err != nil {
		t.Fatalf("DiscoverCommands: %v", err)
	}
	c := findCommand(cmds, "review")
	if c == nil {
		t.Fatal("review command not discovered")
	}
	if c.Template != "child review" {
		t.Errorf("nearer dir should win; template = %q, want %q", c.Template, "child review")
	}
}

func TestDiscoverCommands_StopsAtGitRoot(t *testing.T) {
	root := t.TempDir()
	makeCommandFile(t, root, "outside.md", "should not be found")

	gitRoot := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(gitRoot, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	workdir := filepath.Join(gitRoot, "src")
	if err := os.MkdirAll(workdir, 0755); err != nil {
		t.Fatal(err)
	}

	cmds, err := config.DiscoverCommands(workdir)
	if err != nil {
		t.Fatalf("DiscoverCommands: %v", err)
	}
	if findCommand(cmds, "outside") != nil {
		t.Errorf("commands above the git root should not be discovered, got %+v", cmds)
	}
}

func TestExpandCommand(t *testing.T) {
	cases := []struct {
		name     string
		template string
		args     []string
		want     string
	}{
		{"arguments join", "Context: $ARGUMENTS", []string{"a", "b", "c"}, "Context: a b c"},
		{"positional", "issue #$1 by $2", []string{"42", "srjn"}, "issue #42 by srjn"},
		{"out of range empty", "first=$1 third=$3", []string{"x"}, "first=x third="},
		{"dollar escape", "cost is $$5", nil, "cost is $5"},
		{"untouched non-arg", "run ${HOME}/bin and $PATH", nil, "run ${HOME}/bin and $PATH"},
		{"multi-digit positional", "$10 vs $1", []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "ten"}, "ten vs 1"},
		{"trailing dollar", "ends with $", nil, "ends with $"},
		{"arguments then text", "$ARGUMENTS!", []string{"hi"}, "hi!"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := config.ExpandCommand(tc.template, tc.args); got != tc.want {
				t.Errorf("ExpandCommand(%q, %v) = %q, want %q", tc.template, tc.args, got, tc.want)
			}
		})
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
