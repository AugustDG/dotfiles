package main

import (
	"fmt"

	"github.com/AugustDG/dotfiles/internal/bootstrap"
	"github.com/AugustDG/dotfiles/internal/platform"
	"github.com/spf13/cobra"
)

func initCmd() *cobra.Command {
	var adopt bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Bootstrap the toolchain, then link modules",
		Long: "Installs Homebrew and the toolchain from dotfiles.toml, sets up gh auth,\\n" +
			"clones the repo and backs up conflicting files, then links modules. A\\n" +
			"terminal gets a picker; without one, every OS-compatible module is linked.\\n" +
			"Safe to rerun. To link individual modules later, use `dotfiles link`.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dotfilesDir := platform.DotfilesDir()

			fmt.Println()
			if err := bootstrap.NewInstaller(nil, dotfilesDir).RunBootstrap(); err != nil {
				return fmt.Errorf("bootstrap failed: %w", err)
			}
			fmt.Println()

			return runLink(dotfilesDir, linkOptions{all: !platform.IsInteractive(), adopt: adopt}, nil)
		},
	}

	cmd.Flags().BoolVar(&adopt, "adopt", false, "On conflict, absorb existing target files into the repo (stow --adopt), then symlink")
	return cmd
}
