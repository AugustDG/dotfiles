package main

import (
	"fmt"

	"github.com/AugustDG/dotfiles/internal/config"
	gitops "github.com/AugustDG/dotfiles/internal/git"
	"github.com/AugustDG/dotfiles/internal/platform"
	"github.com/AugustDG/dotfiles/internal/tui"
	"github.com/spf13/cobra"
)

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show module status",
		RunE: func(cmd *cobra.Command, args []string) error {
			dotfilesDir := platform.DotfilesDir()
			modules, err := config.DiscoverModules(dotfilesDir)
			if err != nil {
				return err
			}

			statuses := moduleStatuses(dotfilesDir, modules)
			fmt.Println(tui.RenderStatusTable(statuses))
			return nil
		},
	}
}

func moduleStatuses(dotfilesDir string, modules []config.Module) []tui.ModuleStatus {
	statuses := make([]tui.ModuleStatus, 0, len(modules))
	for _, mod := range modules {
		status := tui.ModuleStatus{Module: mod}
		if mod.HasSubmodule && len(mod.SubmodulePaths) > 0 {
			state, _ := gitops.SubmoduleStatus(dotfilesDir, mod.SubmodulePaths[0])
			status.SubmoduleState = state
		}
		statuses = append(statuses, status)
	}
	return statuses
}
