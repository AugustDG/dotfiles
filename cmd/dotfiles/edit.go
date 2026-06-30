package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/AugustDG/dotfiles/internal/platform"
	"github.com/spf13/cobra"
)

func editCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit [module]",
		Short: "Open the dotfiles repo (or a module) in your editor",
		Long: "Opens the dotfiles directory, or a specific module's directory, in $VISUAL/\n" +
			"$EDITOR (falling back to nvim/vim/vi/nano). Prints the path instead when no\n" +
			"editor is available or stdout is not a terminal.",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: moduleNameCompletion,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEdit(args)
		},
	}
}

func runEdit(args []string) error {
	target := platform.DotfilesDir()
	if len(args) == 1 {
		target = filepath.Join(target, args[0])
		if fi, err := os.Stat(target); err != nil || !fi.IsDir() {
			return fmt.Errorf("module %q not found", args[0])
		}
	}

	editor := platform.Editor()
	if editor == "" || !platform.IsInteractive() {
		fmt.Println(target)
		return nil
	}

	// $EDITOR may include flags, e.g. "code --wait".
	parts := strings.Fields(editor)
	c := exec.Command(parts[0], append(parts[1:], target)...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}
