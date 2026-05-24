package main

import (
	"fmt"
	"os"

	"github.com/AugustDG/dotfiles/internal/bootstrap"
	"github.com/AugustDG/dotfiles/internal/config"
	gitops "github.com/AugustDG/dotfiles/internal/git"
	"github.com/AugustDG/dotfiles/internal/platform"
	"github.com/AugustDG/dotfiles/internal/runner"
	"github.com/AugustDG/dotfiles/internal/stow"
	"github.com/AugustDG/dotfiles/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	rootCmd := &cobra.Command{
		Use:     "dotfiles",
		Short:   "Manage dotfiles modules",
		Version: version,
	}

	rootCmd.PersistentFlags().BoolVarP(&runner.Verbose, "verbose", "v", false, "Show detailed command output")

	rootCmd.AddCommand(installCmd())
	rootCmd.AddCommand(uninstallCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(updateCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func installCmd() *cobra.Command {
	var all bool
	var skipBootstrap bool

	cmd := &cobra.Command{
		Use:   "install [modules...]",
		Short: "Install dotfiles modules",
		RunE: func(cmd *cobra.Command, args []string) error {
			dotfilesDir := platform.DotfilesDir()
			interactive := platform.IsInteractive() && len(args) == 0 && !all

			if !interactive && !all && len(args) == 0 {
				all = true
			}

			if !skipBootstrap {
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

			var selected []config.Module

			if interactive {
				m := tui.NewModel(modules)
				p := tea.NewProgram(m)
				finalModel, err := p.Run()
				if err != nil {
					return err
				}

				fm := finalModel.(tui.Model)
				if fm.Quitting() {
					return nil
				}
				selected = fm.SelectedModules()
				if len(selected) == 0 {
					fmt.Println("No modules selected.")
					return nil
				}
			} else if all {
				currentOS := platform.DetectOS()
				for _, m := range modules {
					if m.SupportsOS(currentOS) {
						selected = append(selected, m)
					}
				}
			} else {
				nameMap := make(map[string]config.Module)
				for _, m := range modules {
					nameMap[m.Name] = m
				}
				for _, name := range args {
					m, ok := nameMap[name]
					if !ok {
						return fmt.Errorf("unknown module: %s", name)
					}
					selected = append(selected, m)
				}
			}

			return runModuleInstall(dotfilesDir, selected, interactive)
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Install all OS-compatible modules")
	cmd.Flags().BoolVar(&skipBootstrap, "skip-bootstrap", false, "Skip the bootstrap phase")
	return cmd
}

func runModuleInstall(dotfilesDir string, modules []config.Module, interactive bool) error {
	if !interactive {
		return runModuleInstallPlain(dotfilesDir, modules)
	}

	m := tui.NewProgressOnlyModel()
	p := tea.NewProgram(m)

	go func() {
		inst := bootstrap.NewInstaller(p, dotfilesDir)

		for _, mod := range modules {
			result := inst.InstallModule(mod)
			p.Send(tui.ModuleResultMsg{Result: result})
		}

		p.Send(tui.AllDoneMsg{})
	}()

	_, err := p.Run()
	return err
}

func runModuleInstallPlain(dotfilesDir string, modules []config.Module) error {
	inst := bootstrap.NewInstaller(nil, dotfilesDir)

	var results []tui.ModuleResult
	for _, mod := range modules {
		fmt.Printf("  %s... ", mod.Name)
		result := inst.InstallModule(mod)
		results = append(results, result)
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

	fmt.Println()
	for _, r := range results {
		icon := "✓"
		if r.Status == "failed" {
			icon = "x"
		} else if r.Status == "skipped" {
			icon = "~"
		}
		fmt.Printf("  %s %-12s %s\n", icon, r.Name, r.Status)
	}
	return nil
}

func uninstallCmd() *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "uninstall [modules...]",
		Short: "Unstow modules from $HOME",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dotfilesDir := platform.DotfilesDir()
			homeDir := platform.HomeDir()

			var names []string
			if all {
				modules, err := config.DiscoverModules(dotfilesDir)
				if err != nil {
					return err
				}
				for _, m := range modules {
					names = append(names, m.Name)
				}
			} else {
				if len(args) == 0 {
					return fmt.Errorf("requires at least 1 arg(s), or use --all")
				}
				names = args
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

			var statuses []tui.ModuleStatus
			for _, mod := range modules {
				ms := tui.ModuleStatus{Module: mod}
				if mod.HasSubmodule && len(mod.SubmodulePaths) > 0 {
					state, _ := gitops.SubmoduleStatus(dotfilesDir, mod.SubmodulePaths[0])
					ms.SubmoduleState = state
				}
				statuses = append(statuses, ms)
			}

			fmt.Println(tui.RenderStatusTable(statuses))
			return nil
		},
	}
}

func updateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update [modules...]",
		Short: "Pull latest and re-stow modules",
		RunE: func(cmd *cobra.Command, args []string) error {
			dotfilesDir := platform.DotfilesDir()
			homeDir := platform.HomeDir()
			modules, err := config.DiscoverModules(dotfilesDir)
			if err != nil {
				return err
			}

			var targets []config.Module
			if len(args) == 0 {
				targets = modules
			} else {
				nameMap := make(map[string]config.Module)
				for _, m := range modules {
					nameMap[m.Name] = m
				}
				for _, name := range args {
					m, ok := nameMap[name]
					if !ok {
						return fmt.Errorf("unknown module: %s", name)
					}
					targets = append(targets, m)
				}
			}

			for _, mod := range targets {
				if !mod.HasSubmodule {
					fmt.Printf("%-12s skipped (no submodule)\n", mod.Name)
					continue
				}

				if len(mod.SubmodulePaths) == 0 {
					continue
				}

				subDir := mod.SubmodulePaths[0]
				absPath := subDir
				if !os.IsPathSeparator(subDir[0]) {
					absPath = fmt.Sprintf("%s/%s", dotfilesDir, subDir)
				}

				fmt.Printf("%-12s pulling... ", mod.Name)
				if err := gitops.PullSubmodule(absPath); err != nil {
					fmt.Printf("failed: %s\n", err)
					continue
				}
				fmt.Print("stowing... ")
				if err := stow.Stow(dotfilesDir, mod.Name, homeDir); err != nil {
					fmt.Printf("failed: %s\n", err)
					continue
				}
				fmt.Println("done")
			}
			return nil
		},
	}
}

func findSubmodulePaths(dotfilesDir, moduleName string) []string {
	data, err := os.ReadFile(fmt.Sprintf("%s/.gitmodules", dotfilesDir))
	if err != nil {
		return nil
	}
	var paths []string
	for _, line := range splitLines(string(data)) {
		line = trimSpace(line)
		if len(line) > 7 && line[:7] == "path = " {
			p := line[7:]
			if len(p) > len(moduleName)+1 && p[:len(moduleName)+1] == moduleName+"/" {
				paths = append(paths, p)
			}
		}
	}
	return paths
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
