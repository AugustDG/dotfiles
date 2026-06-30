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
		Use:   "update [modules...]",
		Short: "Pull latest and re-stow modules",
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

	targets, err := resolveOptionalModuleArgs(modules, args)
	if err != nil {
		return err
	}

	for _, mod := range targets {
		updateModule(dotfilesDir, homeDir, mod)
	}
	return nil
}

func updateModule(dotfilesDir, homeDir string, mod config.Module) {
	if !mod.HasSubmodule {
		fmt.Printf("%-12s skipped (no submodule)\n", mod.Name)
		return
	}
	if len(mod.SubmodulePaths) == 0 {
		return
	}

	fmt.Printf("%-12s pulling... ", mod.Name)
	if err := gitops.PullSubmodule(absSubmodulePath(dotfilesDir, mod.SubmodulePaths[0])); err != nil {
		fmt.Printf("failed: %s\n", err)
		return
	}

	fmt.Print("stowing... ")
	if err := stow.Stow(dotfilesDir, mod.Name, homeDir); err != nil {
		fmt.Printf("failed: %s\n", err)
		return
	}

	fmt.Println("done")
}

func absSubmodulePath(dotfilesDir, submodulePath string) string {
	if filepath.IsAbs(submodulePath) {
		return submodulePath
	}
	return filepath.Join(dotfilesDir, submodulePath)
}
