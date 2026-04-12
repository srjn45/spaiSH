package spaish

import (
	"os/exec"
	"strings"
)

// knownBuiltins lists bash/zsh built-in commands that don't appear in PATH.
var knownBuiltins = map[string]bool{
	"cd": true, "export": true, "alias": true, "source": true,
	"echo": true, "exit": true, "set": true, "unset": true,
	"eval": true, "exec": true, "read": true, "type": true,
	"which": true, "history": true, "fg": true, "bg": true,
	"jobs": true, "kill": true, "wait": true, "trap": true,
	"printf": true, "test": true, "[": true, "[[": true,
	"true": true, "false": true, "local": true, "return": true,
	"break": true, "continue": true, "declare": true, "typeset": true,
}

// IsNaturalLanguage returns true if line should be routed to AI rather than
// passed through to the shell. A line starting with "?" is not NL — the caller
// handles the "?" prefix separately.
func IsNaturalLanguage(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "?") {
		return false
	}
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return false
	}
	first := parts[0]
	if knownBuiltins[first] {
		return false
	}
	if _, err := exec.LookPath(first); err == nil {
		return false
	}
	return true
}

// PatternResult describes a detected repetitive command pattern.
type PatternResult struct {
	Kind     string   // "alias" | "long" | "script"
	Commands []string // the repeated command(s)
	Count    int      // number of times seen
}

const ringSize = 50

// Ring is a fixed-size ring buffer of recent shell commands.
type Ring struct {
	buf   [ringSize]string
	count int // number of valid entries (capped at ringSize)
	pos   int // index of the next write slot
}

// Add appends a command to the ring buffer.
func (r *Ring) Add(cmd string) {
	r.buf[r.pos%ringSize] = cmd
	r.pos++
	if r.count < ringSize {
		r.count++
	}
}

// Detect checks for repeating patterns in the ring buffer.
// Returns a PatternResult if a pattern meets minCount, otherwise nil.
// Called after every command — returns at most one result.
func (r *Ring) Detect(minCount int) *PatternResult {
	if r.count < minCount {
		return nil
	}
	cmds := r.recent(r.count)

	// Check sequences of 3+ commands first (more specific than single-command repeat).
	for seqLen := 3; seqLen <= r.count/minCount; seqLen++ {
		if len(cmds) < seqLen {
			break
		}
		seq := cmds[len(cmds)-seqLen:]
		c := r.countSequence(cmds, seq)
		if c >= minCount {
			cp := make([]string, seqLen)
			copy(cp, seq)
			return &PatternResult{Kind: "script", Commands: cp, Count: c}
		}
	}

	// Check: same single command repeated minCount+ times
	last := cmds[len(cmds)-1]
	count := 0
	for _, c := range cmds {
		if c == last {
			count++
		}
	}
	if count >= minCount {
		kind := "alias"
		if len(last) > 60 {
			kind = "long"
		}
		return &PatternResult{Kind: kind, Commands: []string{last}, Count: count}
	}

	return nil
}

// recent returns the last n commands in insertion order.
func (r *Ring) recent(n int) []string {
	if n > r.count {
		n = r.count
	}
	result := make([]string, n)
	start := r.pos - n
	for i := 0; i < n; i++ {
		idx := (start + i) % ringSize
		if idx < 0 {
			idx += ringSize
		}
		result[i] = r.buf[idx]
	}
	return result
}

// countSequence counts non-overlapping occurrences of seq in cmds.
func (r *Ring) countSequence(cmds, seq []string) int {
	count := 0
	seqLen := len(seq)
	i := 0
	for i <= len(cmds)-seqLen {
		match := true
		for j := 0; j < seqLen; j++ {
			if cmds[i+j] != seq[j] {
				match = false
				break
			}
		}
		if match {
			count++
			i += seqLen // non-overlapping
		} else {
			i++
		}
	}
	return count
}
