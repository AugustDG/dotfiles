package main

import (
	"github.com/AugustDG/dotfiles/internal/bootstrap"
	"github.com/AugustDG/dotfiles/internal/config"
	"github.com/AugustDG/dotfiles/internal/platform"
	"github.com/AugustDG/dotfiles/internal/tui"
	"github.com/spf13/cobra"
)

func depsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "deps [modules...]",
		Short: "Install package dependencies for modules",
		Long: "Installs the brew/cask (macOS) or apt/dnf (Linux) dependencies declared by\n" +
			"the given modules, skipping anything already present. With no arguments,\n" +
			"covers all OS-compatible modules.",
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: moduleNameCompletion,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeps(args)
		},
	}
}

func runDeps(args []string) error {
	dotfilesDir := platform.DotfilesDir()
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
		targets = compatibleModules(modules, platform.DetectOS())
	}

	var tasks []tui.Task
	for _, mod := range targets {
		if len(bootstrap.DepNames(mod.Deps)) > 0 {
			tasks = append(tasks, bootstrap.DepsTask(mod))
		}
	}

	if len(tasks) == 0 {
		tui.Info("No dependencies to install.")
		return nil
	}

	errs, err := tui.RunTasks("Installing dependencies", tasks)
	if err != nil {
		return err
	}
	return firstError(errs)
}
