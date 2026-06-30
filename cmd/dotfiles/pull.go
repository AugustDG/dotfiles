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

	// Decide which modules to re-stow from the PRE-pull snapshot: the named
	// ones, or every currently-installed module. Capturing it before the pull
	// is deliberate — a module that gains a new file in this pull still reads as
	// installed now, but would look "not stowed" afterwards (the new leaf has no
	// link yet), so re-stowing it post-pull is exactly what links the new file.
	var targetNames []string
	if len(args) > 0 {
		named, err := resolveModuleArgs(modules, args)
		if err != nil {
			return err
		}
		for _, mod := range named {
			targetNames = append(targetNames, mod.Name)
		}
	} else {
		for _, mod := range modules {
			if mod.IsStowed {
				targetNames = append(targetNames, mod.Name)
			}
		}
	}

	tasks := []tui.Task{
		{Title: "Pull dotfiles repo", Run: func(context.Context) error { return gitops.Pull(dotfilesDir) }},
		{Title: "Sync submodules", Run: func(context.Context) error { return gitops.SyncSubmodules(dotfilesDir) }},
	}
	for _, name := range targetNames {
		name := name
		tasks = append(tasks, tui.Task{
			Title: "Stow " + name,
			Run:   func(context.Context) error { return stow.Stow(dotfilesDir, name, homeDir) },
		})
	}

	errs, err := tui.RunTasks("Pulling dotfiles", tasks)
	if err != nil {
		return err
	}
	return firstError(errs)
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
