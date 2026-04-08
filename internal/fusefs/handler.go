package fusefs

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"spaios/internal/protocol"
	"spaios/internal/socket"
)

const maxFileBytes = 512 * 1024

// ValidOps is the set of virtual operations exposed under /ai.
var ValidOps = map[string]bool{
	"explain":   true,
	"summarise": true,
	"fix":       true,
	"security":  true,
	"ask":       true,
}

// ParsedPath holds the result of parsing a FUSE virtual path.
type ParsedPath struct {
	Op     string // "explain" | "summarise" | "fix" | "security" | "ask"
	Target string // real filesystem path for file ops; question string for "ask"
	IsAsk  bool
}

// ParsePath parses a virtual FUSE path (with the /ai prefix already stripped
// by the FUSE layer) into an Op and target.
//
// Input examples:
//
//	"/explain/etc/nginx/nginx.conf"   → {Op:"explain", Target:"/etc/nginx/nginx.conf"}
//	"/ask/what is using port 8080"    → {Op:"ask", Target:"what is using port 8080", IsAsk:true}
func ParsePath(path string) (ParsedPath, error) {
	trimmed := strings.TrimPrefix(path, "/")
	idx := strings.Index(trimmed, "/")
	if idx < 0 {
		return ParsedPath{}, fmt.Errorf("[spaiOS error: path too short %q — expected /<op>/<target>]", path)
	}
	op := trimmed[:idx]
	rest := trimmed[idx:] // includes leading slash

	if !ValidOps[op] {
		return ParsedPath{}, fmt.Errorf("[spaiOS error: unknown operation '%s' — valid: explain, summarise, fix, security, ask]", op)
	}

	if op == "ask" {
		question := strings.TrimPrefix(rest, "/")
		if question == "" {
			return ParsedPath{}, fmt.Errorf("[spaiOS error: ask requires a question in the path, e.g. /ai/ask/what is using port 80]")
		}
		return ParsedPath{Op: op, Target: question, IsAsk: true}, nil
	}

	return ParsedPath{Op: op, Target: rest}, nil
}

// ReadFile reads the file at path, up to 512 KB.
// Appends "[truncated at 512KB]" if the file exceeds the limit.
// Returns an error whose message is a user-readable spaiOS error string.
func ReadFile(path string) (string, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("[spaiOS error: file not found: %s]", path)
	}
	if os.IsPermission(err) {
		return "", fmt.Errorf("[spaiOS error: permission denied: %s]", path)
	}
	if err != nil {
		return "", fmt.Errorf("[spaiOS error: cannot read %s: %v]", path, err)
	}
	defer f.Close()

	lr := io.LimitReader(f, int64(maxFileBytes)+1)
	data, err := io.ReadAll(lr)
	if err != nil {
		return "", fmt.Errorf("[spaiOS error: read error %s: %v]", path, err)
	}
	if len(data) > maxFileBytes {
		return string(data[:maxFileBytes]) + "[truncated at 512KB]", nil
	}
	return string(data), nil
}

// ReadCallerEnv reads environment variables from /proc/<pid>/environ.
// Returns an empty map on error (caller has no accessible /proc entry).
func ReadCallerEnv(pid uint32) map[string]string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
	if err != nil {
		return map[string]string{}
	}
	env := map[string]string{}
	for _, entry := range bytes.Split(data, []byte{0}) {
		parts := bytes.SplitN(entry, []byte("="), 2)
		if len(parts) == 2 && len(parts[0]) > 0 {
			env[string(parts[0])] = string(parts[1])
		}
	}
	return env
}

// ResolveTimeout returns the effective timeout for a FUSE request.
//
// Priority: SPAI_TIMEOUT in callerEnv > defaultSeconds > 60s hardcoded default.
// Returns 0 to indicate no timeout (when SPAI_TIMEOUT=0 is explicit).
func ResolveTimeout(callerEnv map[string]string, defaultSeconds int) time.Duration {
	if val, ok := callerEnv["SPAI_TIMEOUT"]; ok {
		n, err := strconv.Atoi(val)
		if err == nil {
			if n == 0 {
				return 0 // explicit no-timeout
			}
			return time.Duration(n) * time.Second
		}
	}
	if defaultSeconds > 0 {
		return time.Duration(defaultSeconds) * time.Second
	}
	return 60 * time.Second
}

// Handler performs AI calls via the spaid Unix socket for FUSE operations.
type Handler struct {
	SockPath       string
	DefaultTimeout time.Duration // from [fuse].timeout_seconds; 0 falls back to 60s
}

// Call performs a FUSE AI operation and returns the response as bytes.
// On any error (file not found, daemon unavailable, timeout), the returned
// bytes contain a human-readable "[spaiOS error: ...]" string — never nil.
func (h *Handler) Call(pp ParsedPath, callerEnv map[string]string) []byte {
	timeout := ResolveTimeout(callerEnv, int(h.DefaultTimeout.Seconds()))

	content := ""
	if !pp.IsAsk {
		var err error
		content, err = ReadFile(pp.Target)
		if err != nil {
			return []byte(err.Error())
		}
	}

	req := &protocol.Request{
		Type: "fuse",
		Fuse: &protocol.FuseRequest{
			Op:             pp.Op,
			FileName:       pp.Target,
			Content:        content,
			TimeoutSeconds: int(timeout.Seconds()),
		},
	}

	client := socket.NewClient(h.SockPath)
	var result strings.Builder
	err := client.Send(req, func(resp protocol.Response) error {
		switch resp.Type {
		case "text", "error":
			result.WriteString(resp.Content)
		}
		return nil
	})
	if err != nil {
		return []byte("[spaiOS error: daemon not running — run 'spai mount' or check 'systemctl --user status spaid']")
	}
	return []byte(result.String())
}
