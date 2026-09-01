package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/AugustDG/dotfiles/internal/config"
	gitops "github.com/AugustDG/dotfiles/internal/git"
	"github.com/AugustDG/dotfiles/internal/platform"
	"github.com/AugustDG/dotfiles/internal/stow"
	"github.com/spf13/cobra"
)

func removeCmd() *cobra.Command {
	var (
		deleteFiles    bool
		withSubmodules bool
	)

	cmd := &cobra.Command{
		Use:   "remove <modules...>",
		Short: "Stop tracking and remove modules",
		Long: "Unstows each module, copies its managed files back into $HOME, and deletes\n" +
			"the module from the dotfiles repository. Use --delete-files to remove the\n" +
			"managed files from $HOME, and --with-submodules for modules containing Git\n" +
			"submodules.",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: moduleNameCompletion,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRemove(args, !deleteFiles, withSubmodules)
		},
	}

	cmd.Flags().BoolVar(&deleteFiles, "delete-files", false, "Also remove the module's managed files from $HOME")
	cmd.Flags().BoolVar(&withSubmodules, "with-submodules", false, "Also unregister and remove nested Git submodules")
	return cmd
}

func runRemove(names []string, keepFiles, withSubmodules bool) error {
	dotfilesDir := platform.DotfilesDir()
	homeDir := platform.HomeDir()

	modules, err := config.DiscoverModules(dotfilesDir)
	if err != nil {
		return err
	}
	selected, err := resolveModuleArgs(modules, names)
	if err != nil {
		return err
	}

	seen := make(map[string]bool, len(selected))
	unique := selected[:0]
	for _, mod := range selected {
		if seen[mod.Name] {
			continue
		}
		seen[mod.Name] = true
		unique = append(unique, mod)
	}
	selected = unique

	// Validate the whole batch before unstowing anything. In particular,
	// preserving files must not discover an overwrite conflict halfway through
	// a multi-module removal.
	for _, mod := range selected {
		if mod.HasSubmodule && !withSubmodules {
			return fmt.Errorf("module %q contains Git submodules; rerun with --with-submodules", mod.Name)
		}
		if mod.HasSubmodule {
			if err := validateSubmodulesForRemoval(dotfilesDir, mod, keepFiles); err != nil {
				return err
			}
		}
		if keepFiles {
			if err := preflightMaterialize(mod.Path, homeDir); err != nil {
				return fmt.Errorf("cannot keep files for %s: %w", mod.Name, err)
			}
		}
	}

	for _, mod := range selected {
		if err := removeModule(dotfilesDir, homeDir, mod, keepFiles, withSubmodules); err != nil {
			return err
		}
		if keepFiles {
			fmt.Printf("Removed module %q; files kept in $HOME.\n", mod.Name)
		} else {
			fmt.Printf("Removed module %q.\n", mod.Name)
		}
	}
	return nil
}

func validateSubmodulesForRemoval(dotfilesDir string, mod config.Module, keepFiles bool) error {
	for _, path := range mod.SubmodulePaths {
		state, err := gitops.SubmoduleStatus(dotfilesDir, path)
		if err != nil {
			return fmt.Errorf("inspect submodule %s: %w", path, err)
		}
		if state == "not-init" && keepFiles {
			return fmt.Errorf("submodule %s is not initialized; initialize it before removing with preserved files", path)
		}
		if state == "dirty" || gitops.PathHasChanges(dotfilesDir, path) {
			return fmt.Errorf("submodule %s has uncommitted changes; commit or stash them before removal", path)
		}
	}
	if err := gitops.ValidateSubmoduleRegistrations(dotfilesDir, mod.SubmodulePaths); err != nil {
		return fmt.Errorf("validate submodules for %s: %w", mod.Name, err)
	}
	return nil
}

func removeModule(dotfilesDir, homeDir string, mod config.Module, keepFiles, withSubmodules bool) error {
	if err := stow.Unstow(dotfilesDir, mod.Name, homeDir); err != nil {
		return fmt.Errorf("unstow %s: %w", mod.Name, err)
	}

	var created []string
	if keepFiles {
		var err error
		created, err = materializeModule(mod.Path, homeDir)
		if err != nil {
			removeCreatedPaths(created)
			if restowErr := stow.Stow(dotfilesDir, mod.Name, homeDir); restowErr != nil {
				return fmt.Errorf("keep files for %s: %w (also failed to restore stow links: %v)", mod.Name, err, restowErr)
			}
			return fmt.Errorf("keep files for %s: %w (stow links restored)", mod.Name, err)
		}
	}

	var submoduleRemoval *gitops.SubmoduleRemoval
	if mod.HasSubmodule && withSubmodules {
		var err error
		submoduleRemoval, err = gitops.PrepareSubmoduleRemoval(dotfilesDir, mod.SubmodulePaths)
		if err != nil {
			removeCreatedPaths(created)
			_ = stow.Stow(dotfilesDir, mod.Name, homeDir)
			return fmt.Errorf("unregister submodules for %s: %w", mod.Name, err)
		}
	}

	if err := os.RemoveAll(mod.Path); err != nil {
		var restoreErr error
		if submoduleRemoval != nil {
			restoreErr = submoduleRemoval.Restore(dotfilesDir)
		}
		removeCreatedPaths(created)
		restowErr := stow.Stow(dotfilesDir, mod.Name, homeDir)
		if restoreErr != nil || restowErr != nil {
			return fmt.Errorf("remove %s: %w (restore submodules: %v; restore stow links: %v)", mod.Name, err, restoreErr, restowErr)
		}
		return fmt.Errorf("remove %s: %w (submodules and stow links restored)", mod.Name, err)
	}
	return nil
}

// preflightMaterialize verifies that preserving a module will not overwrite
// unmanaged files. Existing Stow links (including folded parent-directory
// links) are safe because Unstow removes them before materialization.
func preflightMaterialize(moduleDir, homeDir string) error {
	entries, err := os.ReadDir(moduleDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == "module.toml" || entry.Name() == ".git" {
			continue
		}
		if err := preflightModulePath(
			filepath.Join(moduleDir, entry.Name()),
			filepath.Join(homeDir, entry.Name()),
			homeDir,
		); err != nil {
			return err
		}
	}
	return nil
}

func preflightModulePath(src, dst, homeDir string) error {
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return err
	}
	dstInfo, err := os.Lstat(dst)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if isManagedTarget(src, dst, homeDir) {
		return nil
	}
	if !srcInfo.IsDir() || !dstInfo.IsDir() {
		return fmt.Errorf("%s already exists and is not managed by this module", dst)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == ".git" {
			continue
		}
		if err := preflightModulePath(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name()), homeDir); err != nil {
			return err
		}
	}
	return nil
}

func isManagedTarget(src, dst, homeDir string) bool {
	if !hasSymlinkComponent(dst, homeDir) {
		return false
	}
	resolvedSrc, srcErr := filepath.EvalSymlinks(src)
	resolvedDst, dstErr := filepath.EvalSymlinks(dst)
	if srcErr == nil && dstErr == nil && resolvedSrc == resolvedDst {
		return true
	}

	// EvalSymlinks fails for a managed symlink whose source is itself dangling.
	// A direct Stow link can still be identified from its immediate target.
	target, err := os.Readlink(dst)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(dst), target)
	}
	return filepath.Clean(target) == filepath.Clean(src)
}

func hasSymlinkComponent(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || !within(path, root) {
		return false
	}
	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return false
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return true
		}
	}
	return false
}

// materializeModule copies a module's managed contents into homeDir, excluding
// module.toml. It returns every newly-created path so the caller can roll back
// a partial copy without touching files that were already present.
func materializeModule(moduleDir, homeDir string) ([]string, error) {
	entries, err := os.ReadDir(moduleDir)
	if err != nil {
		return nil, err
	}

	var created []string
	for _, entry := range entries {
		if entry.Name() == "module.toml" || entry.Name() == ".git" {
			continue
		}
		if err := copyModulePath(
			filepath.Join(moduleDir, entry.Name()),
			filepath.Join(homeDir, entry.Name()),
			moduleDir,
			homeDir,
			&created,
		); err != nil {
			return created, err
		}
	}
	return created, nil
}

func copyModulePath(src, dst, moduleDir, homeDir string, created *[]string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}

	if info.IsDir() {
		dstInfo, dstErr := os.Lstat(dst)
		switch {
		case os.IsNotExist(dstErr):
			if err := os.Mkdir(dst, info.Mode().Perm()); err != nil {
				return err
			}
			*created = append(*created, dst)
		case dstErr != nil:
			return dstErr
		case !dstInfo.IsDir():
			return fmt.Errorf("%s already exists and is not a directory", dst)
		}

		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.Name() == ".git" {
				continue
			}
			if err := copyModulePath(
				filepath.Join(src, entry.Name()),
				filepath.Join(dst, entry.Name()),
				moduleDir,
				homeDir,
				created,
			); err != nil {
				return err
			}
		}
		return nil
	}

	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("%s already exists; refusing to overwrite it", dst)
	} else if !os.IsNotExist(err) {
		return err
	}

	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		if filepath.IsAbs(target) && within(target, moduleDir) {
			rel, err := filepath.Rel(moduleDir, target)
			if err != nil {
				return err
			}
			target = filepath.Join(homeDir, rel)
		}
		if err := os.Symlink(target, dst); err != nil {
			return err
		}
		*created = append(*created, dst)
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unsupported file type: %s", src)
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	*created = append(*created, dst)
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func removeCreatedPaths(paths []string) {
	for i := len(paths) - 1; i >= 0; i-- {
		_ = os.Remove(paths[i])
	}
}
