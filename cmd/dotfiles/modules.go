package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/AugustDG/dotfiles/internal/config"
	"github.com/AugustDG/dotfiles/internal/platform"
	"github.com/spf13/cobra"
)

// moduleNameCompletion provides shell completion of module names for command
// arguments, omitting names already present on the command line.
func moduleNameCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	modules, err := config.DiscoverModules(platform.DotfilesDir())
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var names []string
	for _, m := range modules {
		if !slices.Contains(args, m.Name) && strings.HasPrefix(m.Name, toComplete) {
			names = append(names, m.Name)
		}
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

func resolveModuleArgs(modules []config.Module, names []string) ([]config.Module, error) {
	byName := moduleMap(modules)

	selected := make([]config.Module, 0, len(names))
	for _, name := range names {
		mod, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("unknown module: %s", name)
		}
		selected = append(selected, mod)
	}

	return selected, nil
}

func resolveOptionalModuleArgs(modules []config.Module, names []string) ([]config.Module, error) {
	if len(names) == 0 {
		return modules, nil
	}
	return resolveModuleArgs(modules, names)
}

func compatibleModules(modules []config.Module, osName string) []config.Module {
	selected := make([]config.Module, 0, len(modules))
	for _, mod := range modules {
		if mod.SupportsOS(osName) {
			selected = append(selected, mod)
		}
	}
	return selected
}

func moduleNames(modules []config.Module) []string {
	names := make([]string, 0, len(modules))
	for _, mod := range modules {
		names = append(names, mod.Name)
	}
	return names
}

func moduleMap(modules []config.Module) map[string]config.Module {
	byName := make(map[string]config.Module, len(modules))
	for _, mod := range modules {
		byName[mod.Name] = mod
	}
	return byName
}
