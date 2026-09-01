package config

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/AugustDG/dotfiles/internal/stow"
)

// Module represents a dotfiles module parsed from a module.toml file.
type Module struct {
	Name        string   `toml:"name"`
	Description string   `toml:"description"`
	OS          []string `toml:"os"`
	Deps        Deps     `toml:"deps"`
	Hooks       Hooks    `toml:"hooks"`

	// Computed at runtime
	Path           string
	HasSubmodule   bool
	SubmodulePaths []string
	IsStowed       bool
}

// Deps lists package manager dependencies for a module. Brew/cask apply on
// macOS (and Linuxbrew); apt/dnf apply on Linux with the matching package
// manager present.
type Deps struct {
	Brew []string `toml:"brew"`
	Cask []string `toml:"cask"`
	Apt  []string `toml:"apt"`
	Dnf  []string `toml:"dnf"`
}

// Empty reports whether the module declares no dependencies at all.
func (d Deps) Empty() bool {
	return len(d.Brew) == 0 && len(d.Cask) == 0 && len(d.Apt) == 0 && len(d.Dnf) == 0
}

// Hooks defines lifecycle hooks for a module.
type Hooks struct {
	PostInstall string `toml:"post_install"`
}

// LoadModule parses a module.toml from the given directory.
func LoadModule(path string) (Module, error) {
	tomlPath := filepath.Join(path, "module.toml")

	var m Module
	if _, err := toml.DecodeFile(tomlPath, &m); err != nil {
		return m, err
	}

	m.Path = path
	return m, nil
}

// DiscoverModules scans all top-level directories in dotfilesDir for
// module.toml files, parses each, and computes runtime fields.
func DiscoverModules(dotfilesDir string) ([]Module, error) {
	entries, err := os.ReadDir(dotfilesDir)
	if err != nil {
		return nil, err
	}

	homeDir, _ := os.UserHomeDir()

	// Parse .gitmodules once to get submodule paths.
	submodulePaths := parseGitmodules(filepath.Join(dotfilesDir, ".gitmodules"))

	var modules []Module
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dir := filepath.Join(dotfilesDir, entry.Name())
		tomlPath := filepath.Join(dir, "module.toml")
		if _, err := os.Stat(tomlPath); os.IsNotExist(err) {
			continue
		}

		m, err := LoadModule(dir)
		if err != nil {
			continue
		}

		// Check if any submodule path is inside this module's directory.
		m.HasSubmodule, m.SubmodulePaths = matchingSubmodulePaths(submodulePaths, entry.Name())

		// Check stow status.
		m.IsStowed = stow.IsStowed(dotfilesDir, entry.Name(), homeDir)

		modules = append(modules, m)
	}

	return modules, nil
}

// SupportsOS returns true if the module supports the given operating system,
// or if no OS restriction is specified.
func (m Module) SupportsOS(os string) bool {
	if len(m.OS) == 0 {
		return true
	}
	for _, o := range m.OS {
		if strings.EqualFold(o, os) {
			return true
		}
	}
	return false
}

// parseGitmodules uses Git's config parser so quoted values, comments, and
// other valid .gitmodules syntax are interpreted exactly as Git sees them.
func parseGitmodules(path string) []string {
	out, err := exec.Command("git", "config", "--file", path, "-z", "--get-regexp", `^submodule\..*\.path$`).Output()
	if err != nil {
		return nil
	}

	var paths []string
	for _, record := range bytes.Split(out, []byte{0}) {
		_, value, ok := bytes.Cut(record, []byte{'\n'})
		if ok {
			paths = append(paths, string(value))
		}
	}
	return paths
}

// matchingSubmodulePaths returns whether the module has submodules and the matching paths.
func matchingSubmodulePaths(submodulePaths []string, moduleName string) (bool, []string) {
	var matches []string
	for _, p := range submodulePaths {
		if p == moduleName || strings.HasPrefix(p, moduleName+"/") {
			matches = append(matches, p)
		}
	}
	return len(matches) > 0, matches
}
