package runner

import (
	"io"
	"os"
	"os/exec"
)

var Verbose bool

// ConfigureCmd wires a command's stdout/stderr to the terminal when Verbose is
// set, or discards them otherwise.
func ConfigureCmd(cmd *exec.Cmd) {
	if Verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
	}
}

// SudoPrefix returns the elevation prefix for the current user: nil when
// already root, otherwise []string{"sudo"}.
func SudoPrefix() []string {
	if os.Getuid() == 0 {
		return nil
	}
	return []string{"sudo"}
}

// Sudo builds a command, prepending sudo when not running as root, and applies
// ConfigureCmd. Callers may still override Stdin/Stdout/Stderr afterwards.
func Sudo(name string, args ...string) *exec.Cmd {
	full := append(append(SudoPrefix(), name), args...)
	cmd := exec.Command(full[0], full[1:]...)
	ConfigureCmd(cmd)
	return cmd
}
