package main

import (
	"context"
	"path/filepath"

	"github.com/AugustDG/dotfiles/internal/config"
	gitops "github.com/AugustDG/dotfiles/internal/git"
	"github.com/AugustDG/dotfiles/internal/platform"
	"github.com/AugustDG/dotfiles/internal/stow"
	"github.com/AugustDG/dotfiles/internal/tui"
	"github.com/spf13/cobra"
)

func bumpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "bump [modules...]",
		Short: "Advance submodules to their upstream latest and re-stow",
		Long: "Moves each submodule to the newest commit on its own upstream, past the\n" +
			"commit the dotfiles repo has recorded, then re-stows it. The repo then\n" +
			"shows the new submodule pointers as changes; run `dotfiles sync` to record them.\n" +
			"To match what the repo already records instead, use `dotfiles pull`.",
		ValidArgsFunction: moduleNameCompletion,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBump(args)
		},
	}
}

func runBump(args []string) error {
	dotfilesDir := platform.DotfilesDir()
	homeDir := platform.HomeDir()

	modules, err := config.DiscoverModules(dotfilesDir)
	if err != nil {
		return err
	}

	var targets []config.Module
	if len(args) > 0 {
		targets, err = resolveModuleArgs(modules, args)
		if err != nil {
			return err
		}
	} else {
		for _, mod := range compatibleModules(modules, platform.DetectOS()) {
			if mod.HasSubmodule {
				targets = append(targets, mod)
			}
		}
	}

	var tasks []tui.Task
	for _, mod := range targets {
		tasks = append(tasks, bumpTasks(dotfilesDir, homeDir, mod)...)
	}
	if len(tasks) == 0 {
		tui.Info("No modules with submodules to bump.")
		return nil
	}

	errs, err := tui.RunTasks("Bumping submodules", tasks)
	if err != nil {
		return err
	}
	return firstError(errs)
}

func bumpTasks(dotfilesDir, homeDir string, mod config.Module) []tui.Task {
	if !mod.HasSubmodule || len(mod.SubmodulePaths) == 0 {
		return []tui.Task{{
			Group: mod.Name,
			Title: "Pull upstream",
			Run:   func(context.Context) error { return tui.Skipped("no submodule") },
		}}
	}
	return []tui.Task{
		{
			Group: mod.Name,
			Title: "Pull upstream",
			Run: func(context.Context) error {
				return gitops.PullSubmodule(absSubmodulePath(dotfilesDir, mod.SubmodulePaths[0]))
			},
		},
		{
			Group: mod.Name,
			Title: "Stow",
			Run:   func(context.Context) error { return stow.Stow(dotfilesDir, mod.Name, homeDir) },
		},
	}
}

func absSubmodulePath(dotfilesDir, submodulePath string) string {
	if filepath.IsAbs(submodulePath) {
		return submodulePath
	}
	return filepath.Join(dotfilesDir, submodulePath)
}
