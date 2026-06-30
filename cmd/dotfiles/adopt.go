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

// adoptMove records a file relocated from its original $HOME location (orig)
// into the module (dest), so it can be reversed on failure.
type adoptMove struct{ dest, orig string }

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

	// Reverse every move so a failure never leaves a config moved-but-unstowed
	// (which would silently break the user's live config).
	var moves []adoptMove
	rollback := func() {
		for i := len(moves) - 1; i >= 0; i-- {
			restoreFromModule(moves[i].dest, moves[i].orig, homeDir, absRepo)
		}
	}

	for _, p := range paths {
		mv, err := adoptPath(p, homeDir, moduleDir, absRepo)
		if err != nil {
			rollback()
			return err
		}
		if mv != nil {
			moves = append(moves, *mv)
		}
	}

	if len(moves) == 0 {
		fmt.Println("Nothing to adopt.")
		return nil
	}

	if err := stow.Stow(dotfilesDir, name, homeDir); err != nil {
		rollback()
		return fmt.Errorf("stow %s: %w (changes rolled back)", name, err)
	}
	fmt.Printf("Adopted %d path(s) into %q and stowed.\n", len(moves), name)
	return nil
}

// adoptPath moves a single path into the module. It returns the recorded move,
// or nil when the path was already a managed symlink (nothing to do).
func adoptPath(p, homeDir, moduleDir, absRepo string) (*adoptMove, error) {
	abs, err := filepath.Abs(platform.ExpandHome(p))
	if err != nil {
		return nil, err
	}

	rel, err := filepath.Rel(homeDir, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("%s is not inside $HOME", p)
	}

	info, err := os.Lstat(abs)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}

	// Already a symlink into the repo? Nothing to do.
	if info.Mode()&os.ModeSymlink != 0 {
		if dest, err := os.Readlink(abs); err == nil {
			if !filepath.IsAbs(dest) {
				dest = filepath.Join(filepath.Dir(abs), dest)
			}
			if within(filepath.Clean(dest), absRepo) {
				fmt.Printf("skip %s (already managed)\n", rel)
				return nil, nil
			}
		}
		return nil, fmt.Errorf("%s is a symlink pointing outside the repo; refusing to adopt", p)
	}

	dest := filepath.Join(moduleDir, rel)
	if _, err := os.Lstat(dest); err == nil {
		return nil, fmt.Errorf("%s already exists in the module; remove it or pick another module", dest)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return nil, err
	}
	if err := movePath(abs, dest); err != nil {
		return nil, fmt.Errorf("move %s: %w", p, err)
	}
	fmt.Printf("adopted %s\n", rel)
	return &adoptMove{dest: dest, orig: abs}, nil
}

// restoreFromModule moves dest back to its original $HOME location. It first
// removes any symlink along orig's path that points into the repo — including a
// stow-folded *parent* directory (e.g. ~/.config/foo -> repo/m/.config/foo) —
// so the real path can be rebuilt and the rename does not become a no-op onto
// the same inode.
func restoreFromModule(dest, orig, homeDir, absRepo string) {
	if rel, err := filepath.Rel(homeDir, orig); err == nil {
		cur := homeDir
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			cur = filepath.Join(cur, part)
			fi, err := os.Lstat(cur)
			if err != nil {
				continue
			}
			if fi.Mode()&os.ModeSymlink != 0 {
				if tgt, err := os.Readlink(cur); err == nil {
					if !filepath.IsAbs(tgt) {
						tgt = filepath.Join(filepath.Dir(cur), tgt)
					}
					if within(filepath.Clean(tgt), absRepo) {
						os.Remove(cur)
					}
				}
				break
			}
		}
	}
	os.MkdirAll(filepath.Dir(orig), 0o755)
	_ = movePath(dest, orig)
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
