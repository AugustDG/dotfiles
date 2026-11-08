package tui

import (
	"fmt"
	"strings"

	"github.com/AugustDG/dotfiles/internal/config"
)

type ModuleStatus struct {
	Module         config.Module
	Dirty          bool   // tracked, staged, unstaged, or untracked changes in this module
	SubmoduleState string // "clean", "dirty", "not-init", ""
	DepsChecked    bool   // false when brew is unavailable
	DepsMissing    []string
	Compatible     bool // supports the current OS
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
		return "  " + titleStyle.Render("repo") + "  " + failStyle.Render("detached HEAD")
	}
	parts := []string{}
	if s.Dirty {
		parts = append(parts, failStyle.Render("dirty"))
	}
	if s.NoUpstream {
		parts = append(parts, failStyle.Render("no upstream"))
	}
	if s.Ahead > 0 {
		parts = append(parts, failStyle.Render(fmt.Sprintf("%d unpushed", s.Ahead)))
	}
	if s.Behind > 0 {
		parts = append(parts, failStyle.Render(fmt.Sprintf("%d behind", s.Behind)))
	}
	if len(parts) == 0 {
		parts = append(parts, okStyle.Render("clean and pushed"))
	}
	return "  " + titleStyle.Render(s.Branch) + "  " + strings.Join(parts, mutedStyle.Render(", "))
}

func RenderStatusTable(statuses []ModuleStatus) string {
	var b strings.Builder

	b.WriteString(groupStyle.Render(fmt.Sprintf("  %-12s %-8s %-8s %-10s %-10s %s",
		"Module", "Stowed", "Changes", "Submodule", "Deps", "Description")))
	b.WriteString("\n  " + Rule(73) + "\n")

	for _, s := range statuses {
		// Modules that don't support this OS can't be installed here — dim the
		// whole row and note the requirement so they don't read as installable.
		if !s.Compatible {
			desc := s.Module.Description
			if len(s.Module.OS) > 0 {
				desc += fmt.Sprintf(" (requires %s)", strings.Join(s.Module.OS, "/"))
			}
			b.WriteString(fmt.Sprintf("  %s %s %s %s %s %s\n",
				pad(mutedStyle.Render(s.Module.Name), 12),
				pad(mutedStyle.Render("n/a"), 8),
				pad(changesCell(s), 8),
				pad(mutedStyle.Render("—"), 10),
				pad(mutedStyle.Render("—"), 10),
				mutedStyle.Render(desc),
			))
			continue
		}

		stowed := failStyle.Render("no")
		if s.Module.IsStowed {
			stowed = okStyle.Render("yes")
		}

		sub := mutedStyle.Render("—")
		if s.Module.HasSubmodule {
			switch s.SubmoduleState {
			case "clean":
				sub = okStyle.Render("clean")
			case "dirty":
				sub = failStyle.Render("dirty")
			case "not-init":
				sub = warnStyle.Render("not-init")
			default:
				sub = mutedStyle.Render("unknown")
			}
		}

		b.WriteString(fmt.Sprintf("  %-12s %s %s %s %s %s\n",
			s.Module.Name,
			pad(stowed, 8),
			pad(changesCell(s), 8),
			pad(sub, 10),
			pad(depsCell(s), 10),
			textStyle.Render(s.Module.Description),
		))
	}

	return b.String()
}

func changesCell(s ModuleStatus) string {
	if s.Dirty {
		return failStyle.Render("dirty")
	}
	if !s.Compatible {
		return mutedStyle.Render("clean")
	}
	return okStyle.Render("clean")
}

func depsCell(s ModuleStatus) string {
	if s.Module.Deps.Empty() {
		return mutedStyle.Render("—")
	}
	if !s.DepsChecked {
		return mutedStyle.Render("?")
	}
	if len(s.DepsMissing) == 0 {
		return okStyle.Render("ok")
	}
	return failStyle.Render(fmt.Sprintf("%d missing", len(s.DepsMissing)))
}
