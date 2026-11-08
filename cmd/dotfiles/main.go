package main

import (
	"errors"
	"os"

	"github.com/AugustDG/dotfiles/internal/runner"
	"github.com/AugustDG/dotfiles/internal/tui"
	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		switch {
		case errors.Is(err, tui.ErrAborted):
			tui.Info("Aborted.")
		case errors.Is(err, tui.ErrReported):
		default:
			tui.Fail("%s", err)
		}
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "dotfiles",
		Short:         "Manage dotfiles modules",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.PersistentFlags().BoolVarP(&runner.Verbose, "verbose", "v", false, "Show detailed command output")

	// Commands are listed by group, in the order given here.
	cobra.EnableCommandSorting = false
	addGroup(cmd, "setup", "Set up this machine:", initCmd(), doctorCmd(), selfUpdateCmd())
	addGroup(cmd, "modules", "Link modules into $HOME:", linkCmd(), unlinkCmd(), statusCmd(), depsCmd(), cleanCmd())
	addGroup(cmd, "repo", "Sync the repo:", pullCmd(), bumpCmd(), syncCmd())
	addGroup(cmd, "author", "Author modules:", addCmd(), adoptCmd(), ejectCmd(), editCmd())

	return cmd
}

// addGroup adds commands to root under a titled section of --help.
func addGroup(root *cobra.Command, id, title string, cmds ...*cobra.Command) {
	root.AddGroup(&cobra.Group{ID: id, Title: title})
	for _, c := range cmds {
		c.GroupID = id
		root.AddCommand(c)
	}
}
