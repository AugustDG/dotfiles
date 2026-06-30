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

func updateCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "update [modules...]",
		Short:             "Pull submodules to upstream latest and re-stow",
		ValidArgsFunction: moduleNameCompletion,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdate(args)
		},
	}
}

func runUpdate(args []string) error {
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
		errs = append(errs, updateModule(dotfilesDir, homeDir, mod))
	}
	return firstError(errs)
}

func updateModule(dotfilesDir, homeDir string, mod config.Module) error {
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
