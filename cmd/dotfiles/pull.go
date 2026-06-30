package main

import (
	"context"
	"fmt"

	"github.com/AugustDG/dotfiles/internal/config"
	gitops "github.com/AugustDG/dotfiles/internal/git"
	"github.com/AugustDG/dotfiles/internal/platform"
	"github.com/AugustDG/dotfiles/internal/stow"
	"github.com/AugustDG/dotfiles/internal/tui"
	"github.com/spf13/cobra"
)

func pullCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pull [modules...]",
		Short: "Pull the latest dotfiles, sync submodules, and re-stow",
		Long: "Fast-forwards the dotfiles repo from origin, updates every submodule to\n" +
			"the recorded commit, then re-stows the affected modules so new files are\n" +
			"linked. With no arguments, re-stows all currently-installed modules.",
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: moduleNameCompletion,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPull(args)
		},
	}
	return cmd
}

func runPull(args []string) error {
	dotfilesDir := platform.DotfilesDir()
	homeDir := platform.HomeDir()

	modules, err := config.DiscoverModules(dotfilesDir)
	if err != nil {
		return err
	}

	// Validate any explicit module names up front, before any network or stow
	// work, so a typo fails fast.
	var named []config.Module
	if len(args) > 0 {
		named, err = resolveModuleArgs(modules, args)
		if err != nil {
			return err
		}
	}

	tasks := []tui.Task{
		{Title: "Pull dotfiles repo", Run: func(context.Context) error { return gitops.Pull(dotfilesDir) }},
		{Title: "Sync submodules", Run: func(context.Context) error { return gitops.SyncSubmodules(dotfilesDir) }},
	}

	if len(named) > 0 {
		for _, mod := range named {
			mod := mod
			tasks = append(tasks, tui.Task{
				Title: "Stow " + mod.Name,
				Run:   func(context.Context) error { return stow.Stow(dotfilesDir, mod.Name, homeDir) },
			})
		}
	} else {
		// Re-stow installed modules using post-pull state, so files that
		// appeared in this pull (including freshly-synced submodules) get linked
		// — without stowing modules the user never chose to install.
		tasks = append(tasks, tui.Task{
			Title: "Re-stow installed modules",
			Run:   func(context.Context) error { return restowInstalled(dotfilesDir, homeDir) },
		})
	}

	errs, err := tui.RunTasks("Pulling dotfiles", tasks)
	if err != nil {
		return err
	}
	return firstError(errs)
}

// restowInstalled re-discovers modules after a pull and re-stows every module
// that is currently stowed, so newly-pulled files are linked. stow -R is
// idempotent, and modules that were never installed are left untouched.
func restowInstalled(dotfilesDir, homeDir string) error {
	modules, err := config.DiscoverModules(dotfilesDir)
	if err != nil {
		return err
	}
	var failed []error
	for _, mod := range modules {
		if !mod.IsStowed {
			continue
		}
		if err := stow.Stow(dotfilesDir, mod.Name, homeDir); err != nil {
			failed = append(failed, fmt.Errorf("%s: %w", mod.Name, err))
		}
	}
	return firstError(failed)
}

// firstError returns the first non-nil error in errs, wrapped with a count of
// how many tasks failed, or nil if all succeeded.
func firstError(errs []error) error {
	failed := 0
	var first error
	for _, e := range errs {
		if e != nil {
			failed++
			if first == nil {
				first = e
			}
		}
	}
	if failed == 0 {
		return nil
	}
	if failed == 1 {
		return first
	}
	return fmt.Errorf("%d steps failed; first: %w", failed, first)
}
