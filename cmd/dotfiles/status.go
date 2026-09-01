package main

import (
	"fmt"
	"os"
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

func statusCmd() *cobra.Command {
	var check bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show module status",
		Long: "Prints the dotfiles repo state and a per-module table of pending changes,\n" +
			"stow, submodule, and dependency status. With --check, exits non-zero when\n" +
			"there are uncommitted/unpushed changes or dangling links (useful in prompts/CI).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(check)
		},
	}

	cmd.Flags().BoolVar(&check, "check", false, "Exit non-zero if the repo is not clean, pushed, and link-healthy")
	return cmd
}

func runStatus(check bool) error {
	dotfilesDir := platform.DotfilesDir()
	homeDir := platform.HomeDir()

	modules, err := config.DiscoverModules(dotfilesDir)
	if err != nil {
		return err
	}

	summary, haveRepo := repoSummary(dotfilesDir)
	if haveRepo {
		fmt.Println(tui.RenderRepoSummary(summary))
		fmt.Println()
	}

	statuses := moduleStatuses(dotfilesDir, modules)
	fmt.Println(tui.RenderStatusTable(statuses))

	if !check {
		return nil
	}

	dangling := stow.FindDangling(dotfilesDir, homeDir, stow.ScanRoots(dotfilesDir, homeDir, moduleNames(modules)))
	if (haveRepo && !summary.Clean()) || len(dangling) > 0 {
		return fmt.Errorf("dotfiles not in sync (dirty/unpushed or %d dangling link(s))", len(dangling))
	}
	return nil
}

func repoSummary(dotfilesDir string) (tui.RepoSummary, bool) {
	if _, err := os.Stat(filepath.Join(dotfilesDir, ".git")); err != nil {
		return tui.RepoSummary{}, false
	}
	branch, err := gitops.CurrentBranch(dotfilesDir)
	if err != nil {
		return tui.RepoSummary{Detached: true}, true
	}
	ahead, behind := gitops.AheadBehind(dotfilesDir)
	return tui.RepoSummary{
		Branch:     branch,
		Ahead:      ahead,
		Behind:     behind,
		Dirty:      gitops.IsDirty(dotfilesDir),
		NoUpstream: !gitops.HasUpstream(dotfilesDir),
	}, true
}

func moduleStatuses(dotfilesDir string, modules []config.Module) []tui.ModuleStatus {
	dirtyModules := changedModules(gitops.ChangedPaths(dotfilesDir))

	var formulae, casks map[string]bool
	depsChecked := brew.IsInstalled()
	if depsChecked {
		formulae = brew.InstalledFormulae()
		casks = brew.InstalledCasks()
	}

	currentOS := platform.DetectOS()
	statuses := make([]tui.ModuleStatus, 0, len(modules))
	for _, mod := range modules {
		status := tui.ModuleStatus{
			Module:      mod,
			Dirty:       dirtyModules[mod.Name],
			DepsChecked: depsChecked,
			Compatible:  mod.SupportsOS(currentOS),
		}
		if mod.HasSubmodule && len(mod.SubmodulePaths) > 0 {
			state, _ := gitops.SubmoduleStatus(dotfilesDir, mod.SubmodulePaths[0])
			status.SubmoduleState = state
		}
		if depsChecked && !mod.Deps.Empty() {
			status.DepsMissing = bootstrap.MissingDeps(mod.Deps, formulae, casks)
		}
		statuses = append(statuses, status)
	}
	return statuses
}

// changedModules maps pending repo changes to their owning top-level Stow
// module. Changes to repo-level files such as README.md or internal/ remain
// represented by the repo summary without incorrectly marking a module dirty.
func changedModules(paths []string) map[string]bool {
	dirty := make(map[string]bool)
	for _, path := range paths {
		path = filepath.ToSlash(filepath.Clean(path))
		name, _, _ := strings.Cut(path, "/")
		if name != "." && name != "" {
			dirty[name] = true
		}
	}
	return dirty
}
