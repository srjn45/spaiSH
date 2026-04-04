package executor

import (
	"io"
	"os/exec"
)

// Execute runs command in a bash shell and streams combined stdout+stderr to w.
// Returns an error if the command exits with a non-zero status.
func Execute(command string, w io.Writer) error {
	cmd := exec.Command("bash", "-c", command)
	cmd.Stdout = w
	cmd.Stderr = w
	return cmd.Run()
}
