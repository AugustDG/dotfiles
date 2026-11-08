package main

import (
	"context"
	"fmt"

	"github.com/AugustDG/dotfiles/internal/config"
	"github.com/AugustDG/dotfiles/internal/platform"
	"github.com/AugustDG/dotfiles/internal/stow"
	"github.com/AugustDG/dotfiles/internal/tui"
	"github.com/spf13/cobra"
)

func unlinkCmd() *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "unlink [modules...]",
		Short: "Remove modules' symlinks from $HOME, keeping them in the repo",
		Long: "Removes each module's symlinks from $HOME. The module stays in the\n" +
			"repository, so `dotfiles link` links it again. To stop managing a\n" +
			"module and keep its files, use `dotfiles eject`.",
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: moduleNameCompletion,
		RunE: func(cmd *cobra.Command, args []string) error {
			dotfilesDir := platform.DotfilesDir()
			homeDir := platform.HomeDir()

			names, err := unlinkTargets(dotfilesDir, args, all)
			if err != nil {
				return err
			}

			tasks := make([]tui.Task, 0, len(names))
			for _, name := range names {
				tasks = append(tasks, tui.Task{
					Title: "Unstow " + name,
					Run:   func(context.Context) error { return stow.Unstow(dotfilesDir, name, homeDir) },
				})
			}
			errs, err := tui.RunTasks("Unlinking modules", tasks)
			if err != nil {
				return err
			}
			return firstError(errs)
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Unstow all modules")
	return cmd
}

func unlinkTargets(dotfilesDir string, args []string, all bool) ([]string, error) {
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
