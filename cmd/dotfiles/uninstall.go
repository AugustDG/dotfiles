package main

import (
	"fmt"

	"github.com/AugustDG/dotfiles/internal/config"
	"github.com/AugustDG/dotfiles/internal/platform"
	"github.com/AugustDG/dotfiles/internal/stow"
	"github.com/spf13/cobra"
)

func uninstallCmd() *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:               "uninstall [modules...]",
		Short:             "Unstow modules from $HOME",
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: moduleNameCompletion,
		RunE: func(cmd *cobra.Command, args []string) error {
			dotfilesDir := platform.DotfilesDir()
			homeDir := platform.HomeDir()

			names, err := uninstallTargets(dotfilesDir, args, all)
			if err != nil {
				return err
			}

			for _, name := range names {
				fmt.Printf("Unstowing %s... ", name)
				if err := stow.Unstow(dotfilesDir, name, homeDir); err != nil {
					fmt.Printf("failed: %s\n", err)
				} else {
					fmt.Println("done")
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Unstow all modules")
	return cmd
}

func uninstallTargets(dotfilesDir string, args []string, all bool) ([]string, error) {
	if all {
		modules, err := config.DiscoverModules(dotfilesDir)
		if err != nil {
			return nil, err
		}
		return moduleNames(modules), nil
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("requires at least 1 arg(s), or use --all")
	}
	return args, nil
}
