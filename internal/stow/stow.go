package stow

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/AugustDG/dotfiles/internal/runner"
)

// skipFiles lists filenames that should be ignored when checking stow status.
var skipFiles = map[string]bool{
	"module.toml":  true,
	".git":         true,
	".gitmodules":  true,
	".gitignore":   true,
	"README.md":    true,
	"LICENSE":      true,
}

// IsStowed returns true if every leaf file in the module directory (excluding
// metadata files) has a corresponding symlink under homeDir pointing back to
// the dotfiles source.
func IsStowed(dotfilesDir, moduleName, homeDir string) bool {
	moduleDir := filepath.Join(dotfilesDir, moduleName)

	allLinked := true
	hasFiles := false

	filepath.Walk(moduleDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			allLinked = false
			return nil
		}

		// Get the path relative to the module directory.
		rel, err := filepath.Rel(moduleDir, path)
		if err != nil {
			allLinked = false
			return nil
		}

		// Skip root directory itself.
		if rel == "." {
			return nil
		}

		// Skip metadata files and directories.
		base := filepath.Base(path)
		if skipFiles[base] {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// We only check leaf files.
		if info.IsDir() {
			return nil
		}

		hasFiles = true
		targetPath := filepath.Join(homeDir, rel)

		absSource, _ := filepath.Abs(path)

		if !isLinkedToSource(targetPath, absSource) {
			allLinked = false
		}

		return nil
	})

	return hasFiles && allLinked
}

// isLinkedToSource checks whether targetPath resolves to absSource, either via
// a direct symlink on the file itself or via a symlinked parent directory.
func isLinkedToSource(targetPath, absSource string) bool {
	// Fast path: the file itself is a symlink.
	if linkDest, err := os.Readlink(targetPath); err == nil {
		if !filepath.IsAbs(linkDest) {
			linkDest = filepath.Join(filepath.Dir(targetPath), linkDest)
		}
		absLink, _ := filepath.Abs(linkDest)
		return absLink == absSource
	}

	// Slow path: check if any ancestor directory is a symlink that, when
	// resolved, makes the full path point to absSource.
	resolved, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		return false
	}
	absResolved, _ := filepath.Abs(resolved)
	return absResolved == absSource
}

// Stow runs GNU stow to create symlinks for the given module.
func Stow(dotfilesDir, moduleName, homeDir string) error {
	cmd := exec.Command("stow", "-d", dotfilesDir, "-t", homeDir,
		"--ignore=module.toml", "-R", moduleName)
	runner.ConfigureCmd(cmd)
	return cmd.Run()
}

// Unstow runs GNU stow to remove symlinks for the given module.
func Unstow(dotfilesDir, moduleName, homeDir string) error {
	cmd := exec.Command("stow", "-d", dotfilesDir, "-t", homeDir,
		"--ignore=module.toml", "-D", moduleName)
	runner.ConfigureCmd(cmd)
	return cmd.Run()
}

