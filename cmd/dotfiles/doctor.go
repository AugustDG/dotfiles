package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/AugustDG/dotfiles/internal/bootstrap"
	"github.com/AugustDG/dotfiles/internal/brew"
	"github.com/AugustDG/dotfiles/internal/config"
	gitops "github.com/AugustDG/dotfiles/internal/git"
	"github.com/AugustDG/dotfiles/internal/platform"
	"github.com/AugustDG/dotfiles/internal/stow"
	"github.com/AugustDG/dotfiles/internal/tui"
	"github.com/spf13/cobra"
)

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose the dotfiles setup and report problems",
		Long: "Runs a series of health checks — required tools, repo state, GitHub auth,\n" +
			"default shell, PATH, module dependencies, submodules, and dangling links —\n" +
			"and exits non-zero if any check fails.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor()
		},
	}
}

func runDoctor() error {
	dotfilesDir := platform.DotfilesDir()
	homeDir := platform.HomeDir()

	checks := environmentChecks(dotfilesDir, homeDir)

	modules, err := config.DiscoverModules(dotfilesDir)
	if err != nil {
		checks = append(checks, tui.Check{Name: "modules", Level: tui.CheckFail, Detail: err.Error()})
	} else {
		checks = append(checks, moduleChecks(dotfilesDir, modules)...)
		checks = append(checks, danglingCheck(dotfilesDir, homeDir, modules))
	}

	fmt.Println(tui.RenderChecks("dotfiles doctor", checks))

	fails := 0
	for _, c := range checks {
		if c.Level == tui.CheckFail {
			fails++
		}
	}
	if fails > 0 {
		return fmt.Errorf("%d check(s) failed", fails)
	}
	return nil
}

// environmentChecks covers tools, repo, auth, shell, and PATH.
func environmentChecks(dotfilesDir, homeDir string) []tui.Check {
	var checks []tui.Check

	checks = append(checks,
		toolCheck("git", true),
		toolCheck("stow", true),
		toolCheck("brew", false),
		toolCheck("gh", false),
	)

	if _, err := os.Stat(filepath.Join(dotfilesDir, ".git")); err != nil {
		checks = append(checks, tui.Check{Name: "repo", Level: tui.CheckFail,
			Detail: dotfilesDir + " is not a git repository"})
	} else {
		checks = append(checks, repoStateCheck(dotfilesDir))
	}

	if gitops.IsGHAuthenticated() {
		checks = append(checks, tui.Check{Name: "github auth", Level: tui.CheckOK, Detail: "authenticated"})
	} else {
		checks = append(checks, tui.Check{Name: "github auth", Level: tui.CheckWarn,
			Detail: "not logged in (run: gh auth login)"})
	}

	checks = append(checks, shellCheck(), localBinCheck(), localRcCheck(homeDir))
	return checks
}

func toolCheck(name string, required bool) tui.Check {
	if path, err := exec.LookPath(name); err == nil {
		return tui.Check{Name: name, Level: tui.CheckOK, Detail: path}
	}
	level := tui.CheckWarn
	if required {
		level = tui.CheckFail
	}
	return tui.Check{Name: name, Level: level, Detail: "not found on PATH"}
}

func repoStateCheck(dotfilesDir string) tui.Check {
	branch, err := gitops.CurrentBranch(dotfilesDir)
	if err != nil {
		return tui.Check{Name: "repo state", Level: tui.CheckWarn, Detail: "detached HEAD"}
	}
	ahead, behind := gitops.AheadBehind(dotfilesDir)
	dirty := gitops.IsDirty(dotfilesDir)

	var notes []string
	if dirty {
		notes = append(notes, "uncommitted changes")
	}
	if !gitops.HasUpstream(dotfilesDir) {
		notes = append(notes, "no upstream tracking branch")
	}
	if ahead > 0 {
		notes = append(notes, fmt.Sprintf("%d unpushed", ahead))
	}
	if behind > 0 {
		notes = append(notes, fmt.Sprintf("%d behind", behind))
	}
	if len(notes) == 0 {
		return tui.Check{Name: "repo state", Level: tui.CheckOK, Detail: branch + ", clean and pushed"}
	}
	return tui.Check{Name: "repo state", Level: tui.CheckWarn,
		Detail: branch + ": " + strings.Join(notes, ", ")}
}

func shellCheck() tui.Check {
	shell := os.Getenv("SHELL")
	if strings.HasSuffix(shell, "zsh") {
		return tui.Check{Name: "default shell", Level: tui.CheckOK, Detail: shell}
	}
	detail := "not zsh"
	if shell != "" {
		detail = shell + " (expected zsh)"
	}
	return tui.Check{Name: "default shell", Level: tui.CheckWarn, Detail: detail}
}

func localBinCheck() tui.Check {
	bin := platform.LocalBin()
	if platform.PathContains(bin) {
		return tui.Check{Name: "~/.local/bin", Level: tui.CheckOK, Detail: "on PATH"}
	}
	return tui.Check{Name: "~/.local/bin", Level: tui.CheckWarn, Detail: "not on PATH"}
}

func localRcCheck(homeDir string) tui.Check {
	if _, err := os.Stat(filepath.Join(homeDir, ".zshrc.local")); err == nil {
		return tui.Check{Name: "~/.zshrc.local", Level: tui.CheckOK, Detail: "present"}
	}
	return tui.Check{Name: "~/.zshrc.local", Level: tui.CheckWarn,
		Detail: "missing (machine-local secrets go here)"}
}

// moduleChecks reports, for each installed module, its dependency and submodule
// health. Modules that aren't stowed are summarised in a single line.
func moduleChecks(dotfilesDir string, modules []config.Module) []tui.Check {
	var formulae, casks map[string]bool
	if brew.IsInstalled() {
		formulae = brew.InstalledFormulae()
		casks = brew.InstalledCasks()
	}

	var checks []tui.Check
	notInstalled := 0
	for _, mod := range modules {
		if !mod.SupportsOS(platform.DetectOS()) {
			continue
		}
		if !mod.IsStowed {
			notInstalled++
			continue
		}

		var notes []string
		level := tui.CheckOK

		if formulae != nil {
			if missing := bootstrap.MissingDeps(mod.Deps, formulae, casks); len(missing) > 0 {
				notes = append(notes, "missing deps: "+strings.Join(missing, ", "))
				level = tui.CheckWarn
			}
		}
		if mod.HasSubmodule && len(mod.SubmodulePaths) > 0 {
			state, _ := gitops.SubmoduleStatus(dotfilesDir, mod.SubmodulePaths[0])
			switch state {
			case "not-init":
				notes = append(notes, "submodule not initialised")
				level = tui.CheckWarn
			case "dirty":
				notes = append(notes, "submodule has local changes")
				if level == tui.CheckOK {
					level = tui.CheckWarn
				}
			}
		}

		detail := "stowed"
		if len(notes) > 0 {
			detail = strings.Join(notes, "; ")
		}
		checks = append(checks, tui.Check{Name: "module " + mod.Name, Level: level, Detail: detail})
	}

	if notInstalled > 0 {
		checks = append(checks, tui.Check{Name: "modules", Level: tui.CheckOK,
			Detail: fmt.Sprintf("%d not installed (skipped)", notInstalled)})
	}
	return checks
}

func danglingCheck(dotfilesDir, homeDir string, modules []config.Module) tui.Check {
	roots := stow.ScanRoots(dotfilesDir, homeDir, moduleNames(modules))
	dangling := stow.FindDangling(dotfilesDir, homeDir, roots)
	if len(dangling) == 0 {
		return tui.Check{Name: "dangling links", Level: tui.CheckOK, Detail: "none"}
	}
	return tui.Check{Name: "dangling links", Level: tui.CheckWarn,
		Detail: fmt.Sprintf("%d found (run: dotfiles clean)", len(dangling))}
}
