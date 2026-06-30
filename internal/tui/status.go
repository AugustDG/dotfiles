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
	DepsChecked    bool   // false when brew is unavailable
	DepsMissing    []string
}

// RepoSummary describes the state of the dotfiles repo for the status header.
type RepoSummary struct {
	Branch     string
	Ahead      int
	Behind     int
	Dirty      bool
	Detached   bool
	NoUpstream bool
}

// Clean reports whether the repo has nothing outstanding to commit or push. A
// branch with no upstream is not clean: its commits have nowhere to be pushed.
func (s RepoSummary) Clean() bool {
	return !s.Dirty && !s.Detached && !s.NoUpstream && s.Ahead == 0 && s.Behind == 0
}

// RenderRepoSummary renders a one-line summary of the repo state.
func RenderRepoSummary(s RepoSummary) string {
	if s.Detached {
		return "  " + headerStyle.Render("repo") + "  " + noStyle.Render("detached HEAD")
	}
	parts := []string{}
	if s.Dirty {
		parts = append(parts, noStyle.Render("dirty"))
	}
	if s.NoUpstream {
		parts = append(parts, noStyle.Render("no upstream"))
	}
	if s.Ahead > 0 {
		parts = append(parts, noStyle.Render(fmt.Sprintf("%d unpushed", s.Ahead)))
	}
	if s.Behind > 0 {
		parts = append(parts, noStyle.Render(fmt.Sprintf("%d behind", s.Behind)))
	}
	if len(parts) == 0 {
		parts = append(parts, yesStyle.Render("clean and pushed"))
	}
	return "  " + headerStyle.Render(s.Branch) + "  " + strings.Join(parts, descStyle.Render(", "))
}

func RenderStatusTable(statuses []ModuleStatus) string {
	var b strings.Builder

	b.WriteString(headerStyle.Render(fmt.Sprintf("  %-12s %-8s %-10s %-10s %s",
		"Module", "Stowed", "Submodule", "Deps", "Description")))
	b.WriteString("\n")
	b.WriteString(separatorStyle.Render("  " + strings.Repeat("─", 64)))
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

		b.WriteString(fmt.Sprintf("  %-12s %s %s %s %s\n",
			s.Module.Name,
			padStyled(stowed, 8),
			padStyled(sub, 10),
			padStyled(depsCell(s), 10),
			descStyle.Render(s.Module.Description),
		))
	}

	return b.String()
}

func depsCell(s ModuleStatus) string {
	if s.Module.Deps.Empty() {
		return naStyle.Render("—")
	}
	if !s.DepsChecked {
		return naStyle.Render("?")
	}
	if len(s.DepsMissing) == 0 {
		return yesStyle.Render("ok")
	}
	return failureStyle.Render(fmt.Sprintf("%d missing", len(s.DepsMissing)))
}

// padStyled left-aligns a styled (ANSI-wrapped) string to a visible width,
// since %-Ns counts escape bytes. It pads based on the rendered cell width.
func padStyled(s string, width int) string {
	visible := lipgloss.Width(s)
	if visible >= width {
		return s
	}
	return s + strings.Repeat(" ", width-visible)
}
