package protocol

// Request is sent from spai → spaid over the Unix socket.
// Two types are used: "query" for asking, "execute" for running confirmed commands.
type Request struct {
	Type       string   `json:"type"`                 // "query" | "execute"
	Query      string   `json:"query,omitempty"`      // the user's natural language query
	WorkingDir string   `json:"working_dir"`          // current directory from spai
	GitBranch  string   `json:"git_branch,omitempty"` // current git branch, if any
	ForceLocal bool     `json:"force_local"`          // use local model regardless of config
	DryRun     bool     `json:"dry_run"`              // show plan but never execute
	Commands   []string `json:"commands,omitempty"`   // for "execute": commands to run
}

// Response is streamed from spaid → spai as newline-delimited JSON.
type Response struct {
	Type    string        `json:"type"`              // "text" | "plan" | "output" | "done" | "error"
	Content string        `json:"content,omitempty"` // for "text", "output", "error"
	Plan    []CommandItem `json:"plan,omitempty"`    // for "plan"
}

// CommandItem is a single proposed command with its permission classification.
type CommandItem struct {
	Command string `json:"command"` // the shell command to run
	Tier    string `json:"tier"`    // "read" | "write" | "elevated" | "destructive"
	Display string `json:"display"` // human-readable tier label shown to the user
}
