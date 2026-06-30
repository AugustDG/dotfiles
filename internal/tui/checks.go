package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// CheckLevel is the severity of a doctor check result.
type CheckLevel int

const (
	CheckOK CheckLevel = iota
	CheckWarn
	CheckFail
)

var (
	checkOKStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	checkWarnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	checkFailStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

// Check is a single diagnostic result.
type Check struct {
	Name   string
	Detail string
	Level  CheckLevel
}

func (l CheckLevel) icon() string {
	switch l {
	case CheckWarn:
		return checkWarnStyle.Render("⚠")
	case CheckFail:
		return checkFailStyle.Render("✗")
	default:
		return checkOKStyle.Render("✓")
	}
}

// RenderChecks formats a titled list of checks with aligned names and dimmed
// detail text.
func RenderChecks(title string, checks []Check) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n\n")

	width := 0
	for _, c := range checks {
		if len(c.Name) > width {
			width = len(c.Name)
		}
	}

	for _, c := range checks {
		line := fmt.Sprintf("  %s %-*s", c.Level.icon(), width, c.Name)
		if c.Detail != "" {
			line += "  " + descStyle.Render(c.Detail)
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}
