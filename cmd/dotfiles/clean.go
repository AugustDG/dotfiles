package main

import (
	"fmt"

	"github.com/AugustDG/dotfiles/internal/config"
	"github.com/AugustDG/dotfiles/internal/platform"
	"github.com/AugustDG/dotfiles/internal/stow"
	"github.com/AugustDG/dotfiles/internal/tui"
	"github.com/spf13/cobra"
)

func cleanCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove dangling symlinks left by removed dotfiles",
		Long: "Scans the directories your modules map into and removes broken symlinks\n" +
			"that point into the dotfiles repo — leftovers from files deleted or renamed\n" +
			"in a module. Use --dry-run to preview without deleting.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClean(dryRun)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be removed without deleting")
	return cmd
}

func runClean(dryRun bool) error {
	dotfilesDir := platform.DotfilesDir()
	homeDir := platform.HomeDir()

	modules, err := config.DiscoverModules(dotfilesDir)
	if err != nil {
		return err
	}

	roots := stow.ScanRoots(dotfilesDir, homeDir, moduleNames(modules))
	dangling := stow.FindDangling(dotfilesDir, homeDir, roots)

	if len(dangling) == 0 {
		tui.OK("No dangling links.")
		return nil
	}

	if dryRun {
		tui.Info("Would remove %d dangling link(s):", len(dangling))
		for _, l := range dangling {
			tui.Item("%s → %s", l.Path, l.Target)
		}
		return nil
	}

	removed, err := stow.RemoveDangling(dangling)
	for _, p := range removed {
		tui.Item("removed %s", p)
	}
	if err != nil {
		return fmt.Errorf("removed %d of %d before error: %w", len(removed), len(dangling), err)
	}
	tui.OK("Removed %d dangling link(s)", len(removed))
	return nil
}
