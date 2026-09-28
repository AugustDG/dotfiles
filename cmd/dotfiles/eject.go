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

func ejectCmd() *cobra.Command {
	var (
		deleteFiles    bool
		withSubmodules bool
	)

	cmd := &cobra.Command{
		Use:     "eject <modules...>",
		Aliases: []string{"remove"},
		Short:   "Stop managing modules, keeping their files in $HOME",
		Long: "The reverse of adopt. Unstows each module, copies its managed files back\n" +
			"into $HOME as real files, and deletes the module from the dotfiles\n" +
			"repository. Use --delete-files to drop the files from $HOME too, and\n" +
			"--with-submodules for modules containing Git submodules.\n\n" +
			"To only take a module's symlinks off this machine, use `dotfiles unlink`.",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: moduleNameCompletion,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEject(args, !deleteFiles, withSubmodules)
		},
	}

	cmd.Flags().BoolVar(&deleteFiles, "delete-files", false, "Also remove the module's managed files from $HOME")
	cmd.Flags().BoolVar(&withSubmodules, "with-submodules", false, "Also unregister and remove nested Git submodules")
	return cmd
}

func runEject(names []string, keepFiles, withSubmodules bool) error {
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
		skipped, err := ejectModule(dotfilesDir, homeDir, mod, keepFiles, withSubmodules)
		if err != nil {
			return err
		}
		if keepFiles {
			fmt.Printf("Ejected module %q; files kept in $HOME.\n", mod.Name)
		} else {
			fmt.Printf("Ejected module %q.\n", mod.Name)
		}
		for _, s := range skipped {
			fmt.Printf("  skipped %s\n", s)
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

// ejectModule unstows mod, optionally copies its files into homeDir, and
// deletes it from the repo. It returns the links it skipped while copying.
func ejectModule(dotfilesDir, homeDir string, mod config.Module, keepFiles, withSubmodules bool) ([]string, error) {
	if err := stow.Unstow(dotfilesDir, mod.Name, homeDir); err != nil {
		return nil, fmt.Errorf("unstow %s: %w", mod.Name, err)
	}

	var copied materialized
	if keepFiles {
		var err error
		copied, err = materializeModule(mod.Path, homeDir)
		if err != nil {
			removeCreatedPaths(copied.created)
			if restowErr := stow.Stow(dotfilesDir, mod.Name, homeDir); restowErr != nil {
				return nil, fmt.Errorf("keep files for %s: %w (also failed to restore stow links: %v)", mod.Name, err, restowErr)
			}
			return nil, fmt.Errorf("keep files for %s: %w (stow links restored)", mod.Name, err)
		}
	}
	created := copied.created

	var submoduleRemoval *gitops.SubmoduleRemoval
	if mod.HasSubmodule && withSubmodules {
		var err error
		submoduleRemoval, err = gitops.PrepareSubmoduleRemoval(dotfilesDir, mod.SubmodulePaths)
		if err != nil {
			removeCreatedPaths(created)
			_ = stow.Stow(dotfilesDir, mod.Name, homeDir)
			return nil, fmt.Errorf("unregister submodules for %s: %w", mod.Name, err)
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
			return nil, fmt.Errorf("eject %s: %w (restore submodules: %v; restore stow links: %v)", mod.Name, err, restoreErr, restowErr)
		}
		return nil, fmt.Errorf("eject %s: %w (submodules and stow links restored)", mod.Name, err)
	}
	return copied.skipped, nil
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

// materialized is what copying a module into $HOME produced.
type materialized struct {
	created []string // every new path, so a partial copy can be rolled back
	skipped []string // links left out because their target doesn't exist
}

// materializeModule copies a module's managed contents into homeDir, excluding
// module.toml. Paths that already existed are never touched, and only the
// ones it created are listed for rollback.
func materializeModule(moduleDir, homeDir string) (materialized, error) {
	var res materialized
	entries, err := os.ReadDir(moduleDir)
	if err != nil {
		return res, err
	}

	for _, entry := range entries {
		if entry.Name() == "module.toml" || entry.Name() == ".git" {
			continue
		}
		if err := copyModulePath(
			filepath.Join(moduleDir, entry.Name()),
			filepath.Join(homeDir, entry.Name()),
			moduleDir,
			homeDir,
			&res,
		); err != nil {
			return res, err
		}
	}
	return res, nil
}

func copyModulePath(src, dst, moduleDir, homeDir string, res *materialized) error {
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
			res.created = append(res.created, dst)
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
				res,
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
		return copyModuleLink(src, dst, moduleDir, homeDir, res)
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
	res.created = append(res.created, dst)
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// copyModuleLink recreates the symlink src at dst so it points at the same
// thing it did inside the module. A relative target resolves from the link's
// own directory, so copying it verbatim would point elsewhere once the link
// lives in $HOME. The target is resolved from src, moved into homeDir when it
// is part of the module being copied, then written back in its original
// style: relative stays relative (to dst), absolute stays absolute. A link
// whose target doesn't exist is skipped rather than recreated broken.
func copyModuleLink(src, dst, moduleDir, homeDir string, res *materialized) error {
	target, err := os.Readlink(src)
	if err != nil {
		return err
	}
	resolved := target
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(filepath.Dir(src), resolved)
	}
	resolved = filepath.Clean(resolved)

	if _, err := os.Stat(resolved); err != nil {
		res.skipped = append(res.skipped, fmt.Sprintf("%s: its link target %s doesn't exist", dst, target))
		return nil
	}

	mapped := resolved
	if within(resolved, moduleDir) {
		rel, err := filepath.Rel(moduleDir, resolved)
		if err != nil {
			return err
		}
		mapped = filepath.Join(homeDir, rel)
	}
	if !filepath.IsAbs(target) {
		if mapped, err = filepath.Rel(filepath.Dir(dst), mapped); err != nil {
			return err
		}
	}

	if err := os.Symlink(mapped, dst); err != nil {
		return err
	}
	res.created = append(res.created, dst)
	return nil
}

func removeCreatedPaths(paths []string) {
	for i := len(paths) - 1; i >= 0; i-- {
		_ = os.Remove(paths[i])
	}
}
