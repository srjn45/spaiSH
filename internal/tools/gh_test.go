package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"spaish/internal/permissions"
)

func TestGHTier(t *testing.T) {
	cases := []struct {
		sub  string
		want permissions.Tier
	}{
		// Read-only queries
		{"pr-view", permissions.TierRead},
		{"pr-list", permissions.TierRead},
		{"pr-status", permissions.TierRead},
		// Local mutations
		{"pr-checkout", permissions.TierWrite},
		{"create-branch", permissions.TierWrite},
		// Outward-facing (remote mutations)
		{"push", permissions.TierElevated},
		{"pr-create", permissions.TierElevated},
		{"pr-comment", permissions.TierElevated},
		{"pr-merge", permissions.TierElevated},
		{"pr-close", permissions.TierElevated},
		// Unknown subcommand defaults to Write (safe: requires confirmation)
		{"pr-reopen", permissions.TierWrite},
		{"", permissions.TierWrite},
	}
	for _, tc := range cases {
		got := GHTier(tc.sub)
		if got != tc.want {
			t.Errorf("GHTier(%q) = %v, want %v", tc.sub, got, tc.want)
		}
	}
}

func TestGHCall(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantSub string
		wantLen int
	}{
		{
			name:    "subcommand only",
			input:   `{"subcommand":"pr-view"}`,
			wantSub: "pr-view",
			wantLen: 0,
		},
		{
			name:    "with args array",
			input:   `{"subcommand":"pr-create","args":["--title","My PR","--body","desc"]}`,
			wantSub: "pr-create",
			wantLen: 4,
		},
		{
			name:    "with args string",
			input:   `{"subcommand":"pr-list","args":"--limit 10"}`,
			wantSub: "pr-list",
			wantLen: 2,
		},
		{
			name:    "empty input",
			input:   `{}`,
			wantSub: "",
			wantLen: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sub, args := GHCall(json.RawMessage(tc.input))
			if sub != tc.wantSub {
				t.Errorf("subcommand = %q, want %q", sub, tc.wantSub)
			}
			if len(args) != tc.wantLen {
				t.Errorf("args len = %d, want %d (args=%v)", len(args), tc.wantLen, args)
			}
		})
	}
}

func TestGHInvalidSubcommand(t *testing.T) {
	_, err := (GH{}).Run(context.Background(), json.RawMessage(`{"subcommand":"nuke"}`))
	if err == nil {
		t.Fatal("expected error for unsupported subcommand")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("expected 'unsupported' in error, got %q", err.Error())
	}

	_, err = (GH{}).Run(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for missing subcommand")
	}
}

func TestGHCommand(t *testing.T) {
	cases := []struct {
		sub      string
		args     []string
		wantProg string
		wantArgs []string
	}{
		{"pr-view", nil, "gh", []string{"pr", "view"}},
		{"pr-list", []string{"--limit", "5"}, "gh", []string{"pr", "list", "--limit", "5"}},
		{"pr-status", nil, "gh", []string{"pr", "status"}},
		{"pr-create", []string{"--title", "x"}, "gh", []string{"pr", "create", "--title", "x"}},
		{"pr-merge", []string{"123"}, "gh", []string{"pr", "merge", "123"}},
		{"pr-close", []string{"123"}, "gh", []string{"pr", "close", "123"}},
		{"pr-comment", []string{"--body", "hi"}, "gh", []string{"pr", "comment", "--body", "hi"}},
		{"pr-checkout", []string{"42"}, "gh", []string{"pr", "checkout", "42"}},
		{"create-branch", []string{"feature-x"}, "git", []string{"checkout", "-b", "feature-x"}},
		{"push", []string{"origin", "HEAD"}, "git", []string{"push", "origin", "HEAD"}},
	}
	for _, tc := range cases {
		prog, args := ghCommand(tc.sub, tc.args)
		if prog != tc.wantProg {
			t.Errorf("ghCommand(%q).prog = %q, want %q", tc.sub, prog, tc.wantProg)
		}
		if len(args) != len(tc.wantArgs) {
			t.Errorf("ghCommand(%q).args = %v, want %v", tc.sub, args, tc.wantArgs)
			continue
		}
		for i, a := range args {
			if a != tc.wantArgs[i] {
				t.Errorf("ghCommand(%q).args[%d] = %q, want %q", tc.sub, i, a, tc.wantArgs[i])
			}
		}
	}
}

func TestGHSubcommandAllowList(t *testing.T) {
	// Every subcommand in the allow-list must map to a known tier.
	for _, sub := range ghSubcommands {
		tier := GHTier(sub)
		if tier < permissions.TierRead || tier > permissions.TierDestructive {
			t.Errorf("GHTier(%q) = %v: out of expected range", sub, tier)
		}
	}
	// allowedGHSubcommands and ghSubcommands must stay in sync.
	if len(allowedGHSubcommands) != len(ghSubcommands) {
		t.Errorf("allowedGHSubcommands len %d != ghSubcommands len %d",
			len(allowedGHSubcommands), len(ghSubcommands))
	}
}

func TestGHNotFound(t *testing.T) {
	// Running a subcommand when the binary is absent should surface a Go error,
	// not silently succeed.  We use create-branch (delegates to git) to test the
	// git path and pr-view to test the gh path.
	//
	// We cannot reliably force "not found" in CI, so we only exercise the error
	// path by checking the output from a purposefully bad subcommand output: the
	// validation gate fires before we even reach exec.
	_, err := (GH{}).Run(context.Background(), json.RawMessage(`{"subcommand":"bad-cmd"}`))
	if err == nil {
		t.Fatal("expected error for unknown subcommand before exec")
	}
}

func TestGHSchemaAndDescription(t *testing.T) {
	g := GH{}
	if g.Name() != "gh" {
		t.Errorf("Name() = %q, want %q", g.Name(), "gh")
	}
	desc := g.Description()
	for _, sub := range []string{"pr-view", "pr-create", "pr-list", "push", "create-branch"} {
		if !strings.Contains(desc, sub) {
			t.Errorf("Description() missing %q", sub)
		}
	}
	schema := g.Schema()
	if schema["type"] != "object" {
		t.Errorf("schema type = %v, want object", schema["type"])
	}
}
