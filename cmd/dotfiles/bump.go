package main

import (
	"fmt"
	"path/filepath"

	"github.com/AugustDG/dotfiles/internal/config"
	gitops "github.com/AugustDG/dotfiles/internal/git"
	"github.com/AugustDG/dotfiles/internal/platform"
	"github.com/AugustDG/dotfiles/internal/stow"
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
		targets = compatibleModules(modules, platform.DetectOS())
	}

	var errs []error
	for _, mod := range targets {
		errs = append(errs, bumpModule(dotfilesDir, homeDir, mod))
	}
	return firstError(errs)
}

func bumpModule(dotfilesDir, homeDir string, mod config.Module) error {
	if !mod.HasSubmodule {
		fmt.Printf("%-12s skipped (no submodule)\n", mod.Name)
		return nil
	}
	if len(mod.SubmodulePaths) == 0 {
		return nil
	}

	fmt.Printf("%-12s pulling... ", mod.Name)
	if err := gitops.PullSubmodule(absSubmodulePath(dotfilesDir, mod.SubmodulePaths[0])); err != nil {
		fmt.Printf("failed: %s\n", err)
		return fmt.Errorf("%s: %w", mod.Name, err)
	}

	fmt.Print("stowing... ")
	if err := stow.Stow(dotfilesDir, mod.Name, homeDir); err != nil {
		fmt.Printf("failed: %s\n", err)
		return fmt.Errorf("%s: %w", mod.Name, err)
	}

	fmt.Println("done")
	return nil
}

func absSubmodulePath(dotfilesDir, submodulePath string) string {
	if filepath.IsAbs(submodulePath) {
		return submodulePath
	}
	return filepath.Join(dotfilesDir, submodulePath)
}
