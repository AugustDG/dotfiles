package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/AugustDG/dotfiles/internal/platform"
	"github.com/AugustDG/dotfiles/internal/stow"
	"github.com/spf13/cobra"
)

func adoptCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "adopt <name> <path>...",
		Short: "Move existing $HOME configs into a module and stow them",
		Long: "Moves each given file or directory (which must live under $HOME) into the\n" +
			"named module at its $HOME-relative location, then stows the module so the\n" +
			"original paths become symlinks. Creates the module if it does not exist.",
		Args:              cobra.MinimumNArgs(2),
		ValidArgsFunction: moduleNameCompletion,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdopt(args[0], args[1:])
		},
	}
	return cmd
}

func runAdopt(name string, paths []string) error {
	if err := validateModuleName(name); err != nil {
		return err
	}
	dotfilesDir := platform.DotfilesDir()
	homeDir := platform.HomeDir()

	moduleDir, created, err := scaffoldModule(dotfilesDir, name, "", nil)
	if err != nil {
		return err
	}
	if created {
		fmt.Printf("Created module %q\n", name)
	}

	absRepo, _ := filepath.Abs(dotfilesDir)
	adopted := 0
	for _, p := range paths {
		moved, err := adoptPath(p, homeDir, moduleDir, absRepo)
		if err != nil {
			return err
		}
		if moved {
			adopted++
		}
	}

	if adopted == 0 {
		fmt.Println("Nothing to adopt.")
		return nil
	}

	if err := stow.Stow(dotfilesDir, name, homeDir); err != nil {
		return fmt.Errorf("stow %s: %w", name, err)
	}
	fmt.Printf("Adopted %d path(s) into %q and stowed.\n", adopted, name)
	return nil
}

// adoptPath moves a single path into the module. It returns whether a move
// happened (false when the path was already a managed symlink).
func adoptPath(p, homeDir, moduleDir, absRepo string) (bool, error) {
	abs, err := filepath.Abs(platform.ExpandHome(p))
	if err != nil {
		return false, err
	}

	rel, err := filepath.Rel(homeDir, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false, fmt.Errorf("%s is not inside $HOME", p)
	}

	info, err := os.Lstat(abs)
	if err != nil {
		return false, fmt.Errorf("%s: %w", p, err)
	}

	// Already a symlink into the repo? Nothing to do.
	if info.Mode()&os.ModeSymlink != 0 {
		if dest, err := os.Readlink(abs); err == nil {
			if !filepath.IsAbs(dest) {
				dest = filepath.Join(filepath.Dir(abs), dest)
			}
			if within(filepath.Clean(dest), absRepo) {
				fmt.Printf("skip %s (already managed)\n", rel)
				return false, nil
			}
		}
		return false, fmt.Errorf("%s is a symlink pointing outside the repo; refusing to adopt", p)
	}

	dest := filepath.Join(moduleDir, rel)
	if _, err := os.Lstat(dest); err == nil {
		return false, fmt.Errorf("%s already exists in the module; remove it or pick another module", dest)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return false, err
	}
	if err := movePath(abs, dest); err != nil {
		return false, fmt.Errorf("move %s: %w", p, err)
	}
	fmt.Printf("adopted %s\n", rel)
	return true, nil
}

// within reports whether path is equal to or nested under dir.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// movePath renames src to dst, falling back to a recursive copy + delete when
// the two live on different filesystems.
func movePath(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !isCrossDevice(err) {
		return err
	}
	cp := exec.Command("cp", "-a", src, dst)
	if out, err := cp.CombinedOutput(); err != nil {
		return fmt.Errorf("copy: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return os.RemoveAll(src)
}

func isCrossDevice(err error) bool {
	var le *os.LinkError
	if errors.As(err, &le) {
		return errors.Is(le.Err, syscall.EXDEV)
	}
	return false
}
