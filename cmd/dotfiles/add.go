package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AugustDG/dotfiles/internal/platform"
	"github.com/spf13/cobra"
)

func addCmd() *cobra.Command {
	var (
		desc string
		oses []string
	)

	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Scaffold a new empty module",
		Long: "Creates a new module directory with a module.toml. Add files to it and run\n" +
			"`dotfiles install <name>`, or use `dotfiles adopt <name> <path>` to pull an\n" +
			"existing config in.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dotfilesDir := platform.DotfilesDir()
			name := args[0]
			if err := validateModuleName(name); err != nil {
				return err
			}
			dir, created, err := scaffoldModule(dotfilesDir, name, desc, oses)
			if err != nil {
				return err
			}
			if !created {
				return fmt.Errorf("module %q already exists at %s", name, dir)
			}
			fmt.Printf("Created module %q at %s\n", name, dir)
			fmt.Printf("Add files, then run: dotfiles install %s\n", name)
			return nil
		},
	}

	cmd.Flags().StringVar(&desc, "desc", "", "Module description")
	cmd.Flags().StringSliceVar(&oses, "os", nil, "Supported OSes (default darwin,linux)")
	return cmd
}

// validateModuleName rejects names that are not a single safe path segment.
func validateModuleName(name string) error {
	if name == "" {
		return fmt.Errorf("module name cannot be empty")
	}
	if strings.ContainsAny(name, "/\\") || name == "." || name == ".." || strings.HasPrefix(name, ".") {
		return fmt.Errorf("invalid module name %q: must be a single directory name without slashes or leading dot", name)
	}
	return nil
}

// scaffoldModule creates a module directory and module.toml. It returns the
// directory, whether it was newly created (false if a manifest already
// existed), and any error.
func scaffoldModule(dotfilesDir, name, desc string, oses []string) (string, bool, error) {
	moduleDir := filepath.Join(dotfilesDir, name)
	manifest := filepath.Join(moduleDir, "module.toml")
	if _, err := os.Stat(manifest); err == nil {
		return moduleDir, false, nil
	}
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		return "", false, err
	}
	if desc == "" {
		desc = name + " config"
	}
	if len(oses) == 0 {
		oses = []string{"darwin", "linux"}
	}
	quoted := make([]string, len(oses))
	for i, o := range oses {
		quoted[i] = fmt.Sprintf("%q", o)
	}
	content := fmt.Sprintf("name = %q\ndescription = %q\nos = [%s]\n",
		name, desc, strings.Join(quoted, ", "))
	if err := os.WriteFile(manifest, []byte(content), 0o644); err != nil {
		return "", false, err
	}
	return moduleDir, true, nil
}
