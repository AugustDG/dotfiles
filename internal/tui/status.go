package tui

import (
	"fmt"
	"strings"

	"github.com/AugustDG/dotfiles/internal/config"
	"github.com/charmbracelet/lipgloss"
)

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	yesStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	noStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	naStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

type ModuleStatus struct {
	Module         config.Module
	SubmoduleState string // "clean", "dirty", "not-init", ""
}

func RenderStatusTable(statuses []ModuleStatus) string {
	var b strings.Builder

	b.WriteString(headerStyle.Render(fmt.Sprintf("  %-12s %-8s %-12s %s", "Module", "Stowed", "Submodule", "Description")))
	b.WriteString("\n")
	b.WriteString(separatorStyle.Render("  " + strings.Repeat("─", 60)))
	b.WriteString("\n")

	for _, s := range statuses {
		stowed := noStyle.Render("no")
		if s.Module.IsStowed {
			stowed = yesStyle.Render("yes")
		}

		sub := naStyle.Render("—")
		if s.Module.HasSubmodule {
			switch s.SubmoduleState {
			case "clean":
				sub = yesStyle.Render("clean")
			case "dirty":
				sub = failureStyle.Render("dirty")
			case "not-init":
				sub = skippedStyle.Render("not-init")
			default:
				sub = naStyle.Render("unknown")
			}
		}

		b.WriteString(fmt.Sprintf("  %-12s %-8s %-12s %s\n",
			s.Module.Name,
			stowed,
			sub,
			descStyle.Render(s.Module.Description),
		))
	}

	return b.String()
}
