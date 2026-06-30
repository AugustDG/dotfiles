package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	summaryTitle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	successStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	failureStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	skippedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	separatorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

type ModuleResult struct {
	Name    string
	Status  string // "installed", "failed", "skipped"
	Warning string
	Hint    string // optional actionable suggestion shown after a failure
}

type SummaryModel struct {
	results []ModuleResult
}

func NewSummaryModel(results []ModuleResult) SummaryModel {
	return SummaryModel{results: results}
}

func (m SummaryModel) View() string {
	var b strings.Builder

	b.WriteString(summaryTitle.Render("Installation Summary"))
	b.WriteString("\n")
	b.WriteString(separatorStyle.Render(strings.Repeat("─", 50)))
	b.WriteString("\n\n")

	for _, r := range m.results {
		var icon string
		switch r.Status {
		case "installed":
			icon = successStyle.Render("✓")
		case "failed":
			icon = failureStyle.Render("x")
		case "skipped":
			icon = skippedStyle.Render("~")
		}

		line := fmt.Sprintf("  %s %-12s %s", icon, r.Name, r.Status)
		if r.Warning != "" {
			line += fmt.Sprintf("  %s", hintStyle.Render("("+r.Warning+")"))
		}
		b.WriteString(line + "\n")
		if r.Hint != "" {
			b.WriteString(fmt.Sprintf("      %s\n", hintStyle.Render("→ "+r.Hint)))
		}
	}

	return b.String()
}
