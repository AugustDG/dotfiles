package main

import (
	"fmt"

	"github.com/AugustDG/dotfiles/internal/bootstrap"
	"github.com/AugustDG/dotfiles/internal/config"
	"github.com/AugustDG/dotfiles/internal/platform"
	"github.com/AugustDG/dotfiles/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

type installOptions struct {
	all           bool
	skipBootstrap bool
}

func installCmd() *cobra.Command {
	opts := installOptions{}

	cmd := &cobra.Command{
		Use:   "install [modules...]",
		Short: "Install dotfiles modules",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInstall(opts, args)
		},
	}

	cmd.Flags().BoolVar(&opts.all, "all", false, "Install all OS-compatible modules")
	cmd.Flags().BoolVar(&opts.skipBootstrap, "skip-bootstrap", false, "Skip the bootstrap phase")
	return cmd
}

func runInstall(opts installOptions, args []string) error {
	dotfilesDir := platform.DotfilesDir()
	interactive := platform.IsInteractive() && len(args) == 0 && !opts.all
	installAll := opts.all || (!interactive && len(args) == 0)

	if !opts.skipBootstrap {
		fmt.Println()
		inst := bootstrap.NewInstaller(nil, dotfilesDir)
		if err := inst.RunBootstrap(); err != nil {
			return fmt.Errorf("bootstrap failed: %w", err)
		}
		fmt.Println()
	}

	modules, err := config.DiscoverModules(dotfilesDir)
	if err != nil {
		return fmt.Errorf("discover modules: %w", err)
	}

	selected, err := installTargets(modules, args, installAll, interactive)
	if err != nil {
		return err
	}
	if selected == nil {
		return nil
	}

	return runModuleInstall(dotfilesDir, selected, interactive)
}

func installTargets(modules []config.Module, args []string, all, interactive bool) ([]config.Module, error) {
	if interactive {
		selected, ok, err := selectModulesInteractively(modules)
		if err != nil || !ok {
			return nil, err
		}
		if len(selected) == 0 {
			fmt.Println("No modules selected.")
			return nil, nil
		}
		return selected, nil
	}

	if all {
		return compatibleModules(modules, platform.DetectOS()), nil
	}

	return resolveModuleArgs(modules, args)
}

func selectModulesInteractively(modules []config.Module) ([]config.Module, bool, error) {
	model := tui.NewModel(modules)
	program := tea.NewProgram(model)

	finalModel, err := program.Run()
	if err != nil {
		return nil, false, err
	}

	selection := finalModel.(tui.Model)
	if selection.Quitting() {
		return nil, false, nil
	}
	return selection.SelectedModules(), true, nil
}

func runModuleInstall(dotfilesDir string, modules []config.Module, interactive bool) error {
	if !interactive {
		return runModuleInstallPlain(dotfilesDir, modules)
	}

	model := tui.NewProgressOnlyModel()
	program := tea.NewProgram(model)

	go func() {
		inst := bootstrap.NewInstaller(program, dotfilesDir)
		for _, mod := range modules {
			result := inst.InstallModule(mod)
			program.Send(tui.ModuleResultMsg{Result: result})
		}
		program.Send(tui.AllDoneMsg{})
	}()

	_, err := program.Run()
	return err
}

func runModuleInstallPlain(dotfilesDir string, modules []config.Module) error {
	inst := bootstrap.NewInstaller(nil, dotfilesDir)

	results := make([]tui.ModuleResult, 0, len(modules))
	for _, mod := range modules {
		fmt.Printf("  %s... ", mod.Name)
		result := inst.InstallModule(mod)
		results = append(results, result)
		printInstallResult(result)
	}

	fmt.Println()
	for _, result := range results {
		fmt.Printf("  %s %-12s %s\n", installStatusIcon(result.Status), result.Name, result.Status)
	}
	return nil
}

func printInstallResult(result tui.ModuleResult) {
	switch result.Status {
	case "installed":
		if result.Warning != "" {
			fmt.Printf("done (%s)\n", result.Warning)
		} else {
			fmt.Println("done")
		}
	case "skipped":
		fmt.Printf("skipped (%s)\n", result.Warning)
	case "failed":
		fmt.Printf("failed (%s)\n", result.Warning)
	}
}

func installStatusIcon(status string) string {
	switch status {
	case "failed":
		return "x"
	case "skipped":
		return "~"
	default:
		return "✓"
	}
}
