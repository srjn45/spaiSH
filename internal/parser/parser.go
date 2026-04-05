package parser

import (
	"regexp"
	"strings"
)

var bashBlockRe = regexp.MustCompile("(?s)```(?:bash|sh|shell)?\n(.*?)```")

// ParseCommands extracts shell commands from ```bash code blocks in text.
// Comment lines (starting with #) and blank lines are skipped.
func ParseCommands(text string) []string {
	matches := bashBlockRe.FindAllStringSubmatch(text, -1)
	var commands []string
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		for _, line := range strings.Split(strings.TrimSpace(match[1]), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				commands = append(commands, line)
			}
		}
	}
	return commands
}
