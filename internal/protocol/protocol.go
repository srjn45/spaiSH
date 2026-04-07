package protocol

// Request is sent from spai → spaid over the Unix socket.
// Types: "query" | "execute" | "llm" | "agent" | "session" | "confirm_response"
type Request struct {
	Type            string           `json:"type"`                       // "query" | "execute" | "llm" | "agent"
	Query           string           `json:"query,omitempty"`            // the user's natural language query
	WorkingDir      string           `json:"working_dir"`                // current directory from spai
	GitBranch       string           `json:"git_branch,omitempty"`       // current git branch, if any
	ForceLocal      bool             `json:"force_local"`                // use local model regardless of config
	DryRun          bool             `json:"dry_run"`                    // show plan but never execute
	Commands        []string         `json:"commands,omitempty"`         // for "execute": commands to run
	LLM             *LLMRequest      `json:"llm,omitempty"`              // for "llm" request type
	Agent           *AgentRequest    `json:"agent,omitempty"`            // for "agent" request type
	ConfirmResponse *ConfirmResponse `json:"confirm_response,omitempty"` // mid-loop reply from spai
	SessionID       string           `json:"session_id,omitempty"`       // routing key for session file
	Stdin           string           `json:"stdin,omitempty"`            // content from piped stdin
	Session         *SessionRequest  `json:"session,omitempty"`          // for "session" request type
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

// LLMRequest is the payload for "llm" request type.
// Used by the spai CLI to send LLM management commands to spaid.
type LLMRequest struct {
	Command string   `json:"command"` // "status" | "install" | "list" | "pull" | "use"
	Args    []string `json:"args,omitempty"`
}

// AgentRequest is the payload for "agent" request type.
type AgentRequest struct {
	Query      string `json:"query"`
	Verbose    bool   `json:"verbose"`
	Autonomous bool   `json:"autonomous"`
}

// ConfirmRequest is streamed mid-loop from spaid → spai asking the user to
// approve a tier-gated command.
type ConfirmRequest struct {
	Command   string `json:"command"`
	Tier      string `json:"tier"`
	Display   string `json:"display"`
	Iteration int    `json:"iteration"`
}

// ConfirmResponse is sent from spai → spaid after the user decides.
type ConfirmResponse struct {
	Approved bool `json:"approved"`
}

// SessionRequest is the payload for "session" request type.
type SessionRequest struct {
	Command string `json:"command"`         // "clear" | "compact" | "rebuild-context"
	Lines   int    `json:"lines,omitempty"` // for "clear": keep latest N messages (0 = wipe all)
}
