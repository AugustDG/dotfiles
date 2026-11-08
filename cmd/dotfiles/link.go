package main

import (
	"fmt"
	"strings"

	"github.com/AugustDG/dotfiles/internal/bootstrap"
	"github.com/AugustDG/dotfiles/internal/config"
	"github.com/AugustDG/dotfiles/internal/platform"
	"github.com/AugustDG/dotfiles/internal/tui"
	"github.com/spf13/cobra"
)

type linkOptions struct {
	all   bool
	adopt bool
}

func linkCmd() *cobra.Command {
	opts := linkOptions{}

	cmd := &cobra.Command{
		Use:   "link [modules...]",
		Short: "Symlink modules into $HOME, with their deps and hooks",
		Long: "Stows each module, installs its package deps and runs its post_install\n" +
			"hook. With no modules, a terminal gets a picker. The reverse is\n" +
			"`dotfiles unlink`. To set up a new machine, use `dotfiles init`.",
		ValidArgsFunction: moduleNameCompletion,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLink(platform.DotfilesDir(), opts, args)
		},
	}

	cmd.Flags().BoolVar(&opts.all, "all", false, "Link all OS-compatible modules")
	cmd.Flags().BoolVar(&opts.adopt, "adopt", false, "On conflict, absorb existing target files into the repo (stow --adopt), then symlink")
	return cmd
}

// runLink links the named modules, all compatible ones with opts.all, or
// the ones picked interactively when neither is given in a terminal.
func runLink(dotfilesDir string, opts linkOptions, args []string) error {
	interactive := platform.IsInteractive() && len(args) == 0 && !opts.all
	if !interactive && !opts.all && len(args) == 0 {
		return fmt.Errorf("requires at least 1 module, or use --all")
	}

	modules, err := config.DiscoverModules(dotfilesDir)
	if err != nil {
		return fmt.Errorf("discover modules: %w", err)
	}

	selected, err := linkTargets(modules, args, opts.all, interactive)
	if err != nil {
		return err
	}
	if selected == nil {
		return nil
	}

	return runModuleInstall(dotfilesDir, selected, opts.adopt)
}

func linkTargets(modules []config.Module, args []string, all, interactive bool) ([]config.Module, error) {
	if interactive {
		selected, ok, err := tui.PickModules(modules)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, tui.ErrAborted
		}
		if len(selected) == 0 {
			tui.Info("No modules selected.")
			return nil, nil
		}
		return selected, nil
	}

	if all {
		return compatibleModules(modules, platform.DetectOS()), nil
	}

	return resolveModuleArgs(modules, args)
}

func runModuleInstall(dotfilesDir string, modules []config.Module, adopt bool) error {
	inst := bootstrap.NewInstaller(dotfilesDir)
	var tasks []tui.Task
	for _, mod := range modules {
		tasks = append(tasks, inst.ModuleTasks(mod, adopt)...)
	}
	errs, err := tui.RunTasks("Linking modules", tasks)
	if err != nil {
		return err
	}
	return linkResultsError(tasks, errs)
}

// linkResultsError returns a non-zero error when any module failed, so the
// process exit code reflects partial failure (e.g. for CI or shell chaining).
func linkResultsError(tasks []tui.Task, errs []error) error {
	failed := tui.FailedGroups(tasks, errs)
	if len(failed) == 0 {
		return nil
	}
	return tui.Reported(fmt.Errorf("%d module(s) failed to link: %s", len(failed), strings.Join(failed, ", ")))
}
