package main

import (
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

	targets, err := pullTargets(modules, args)
	if err != nil {
		return err
	}

	tasks := []tui.Task{
		{Title: "Pull dotfiles repo", Run: func() error { return gitops.Pull(dotfilesDir) }},
		{Title: "Sync submodules", Run: func() error { return gitops.SyncSubmodules(dotfilesDir) }},
	}
	for _, mod := range targets {
		mod := mod
		tasks = append(tasks, tui.Task{
			Title: "Stow " + mod.Name,
			Run:   func() error { return stow.Stow(dotfilesDir, mod.Name, homeDir) },
		})
	}

	errs, err := tui.RunTasks("Pulling dotfiles", tasks)
	if err != nil {
		return err
	}
	return firstError(errs)
}

// pullTargets returns the modules to re-stow: the named ones, or every
// currently-stowed module when no names are given.
func pullTargets(modules []config.Module, args []string) ([]config.Module, error) {
	if len(args) > 0 {
		return resolveModuleArgs(modules, args)
	}
	var stowed []config.Module
	for _, mod := range modules {
		if mod.IsStowed {
			stowed = append(stowed, mod)
		}
	}
	return stowed, nil
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
