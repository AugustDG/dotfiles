package tui

import (
	"fmt"
	"io"
	"os"
)

// One-line messages for commands that report results without a task list.
// Each prints "  <icon> <message>"; Item prints an indented detail line that
// belongs to the message above it.

// OK reports a success.
func OK(format string, a ...any) { message(os.Stdout, LevelOK, format, a...) }

// Warn reports something that needs attention but did not fail.
func Warn(format string, a ...any) { message(os.Stdout, LevelWarn, format, a...) }

// Fail reports a failure on stderr.
func Fail(format string, a ...any) { message(os.Stderr, LevelFail, format, a...) }

// Info reports a neutral fact, such as a no-op or a dry-run action.
func Info(format string, a ...any) { message(os.Stdout, LevelInfo, format, a...) }

// Item prints a muted detail line under the previous message.
func Item(format string, a ...any) {
	fmt.Println("    " + mutedStyle.Render(fmt.Sprintf(format, a...)))
}

func message(w io.Writer, l Level, format string, a ...any) {
	fmt.Fprintf(w, "  %s %s\n", l.Icon(), fmt.Sprintf(format, a...))
}
