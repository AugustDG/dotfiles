package stow

import (
	"os"
	"os/exec"
	"path/filepath"
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

		linkDest, err := os.Readlink(targetPath)
		if err != nil {
			allLinked = false
			return nil
		}

		// Resolve to absolute for comparison.
		if !filepath.IsAbs(linkDest) {
			linkDest = filepath.Join(filepath.Dir(targetPath), linkDest)
		}

		absSource, _ := filepath.Abs(path)
		absLink, _ := filepath.Abs(linkDest)

		if absSource != absLink {
			allLinked = false
		}

		return nil
	})

	return hasFiles && allLinked
}

// Stow runs GNU stow to create symlinks for the given module.
func Stow(dotfilesDir, moduleName, homeDir string) error {
	cmd := exec.Command("stow", "-d", dotfilesDir, "-t", homeDir, "-R", moduleName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Unstow runs GNU stow to remove symlinks for the given module.
func Unstow(dotfilesDir, moduleName, homeDir string) error {
	cmd := exec.Command("stow", "-d", dotfilesDir, "-t", homeDir, "-D", moduleName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

