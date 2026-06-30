package main

import (
	"os"

	"github.com/AugustDG/dotfiles/internal/runner"
	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "dotfiles",
		Short:        "Manage dotfiles modules",
		Version:      version,
		SilenceUsage: true,
	}

	cmd.PersistentFlags().BoolVarP(&runner.Verbose, "verbose", "v", false, "Show detailed command output")
	cmd.AddCommand(
		installCmd(),
		uninstallCmd(),
		statusCmd(),
		updateCmd(),
		pullCmd(),
		syncCmd(),
		depsCmd(),
		doctorCmd(),
		cleanCmd(),
		addCmd(),
		adoptCmd(),
		editCmd(),
		selfUpdateCmd(),
	)

	return cmd
}
