package tui

import (
	"fmt"
	"strings"
)

// Check is a single diagnostic result.
type Check struct {
	Name   string
	Detail string
	Level  Level
}

// RenderChecks formats a titled list of checks with aligned names and muted
// detail text.
func RenderChecks(title string, checks []Check) string {
	var b strings.Builder
	b.WriteString(Title(title) + "\n\n")

	width := 0
	for _, c := range checks {
		width = max(width, len(c.Name))
	}
	for _, c := range checks {
		line := fmt.Sprintf("  %s %-*s", c.Level.Icon(), width, c.Name)
		if c.Detail != "" {
			line += "  " + mutedStyle.Render(c.Detail)
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}
