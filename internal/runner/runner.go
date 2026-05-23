package runner

import (
	"io"
	"os"
	"os/exec"
)

var Verbose bool

func ConfigureCmd(cmd *exec.Cmd) {
	if Verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
	}
}
