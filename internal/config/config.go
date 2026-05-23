package config

import (
	"bufio"
	"os"
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
	Path         string
	HasSubmodule bool
	IsStowed     bool
}

// Deps lists package manager dependencies for a module.
type Deps struct {
	Brew []string `toml:"brew"`
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
		m.HasSubmodule = hasMatchingSubmodule(submodulePaths, entry.Name())

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

// parseGitmodules reads a .gitmodules file and returns a set of submodule paths.
func parseGitmodules(path string) map[string]bool {
	paths := make(map[string]bool)

	f, err := os.Open(path)
	if err != nil {
		return paths
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "path") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				paths[strings.TrimSpace(parts[1])] = true
			}
		}
	}

	return paths
}

// hasMatchingSubmodule returns true if any submodule path starts with the
// module name (i.e. the module directory contains a submodule).
func hasMatchingSubmodule(submodulePaths map[string]bool, moduleName string) bool {
	for p := range submodulePaths {
		if p == moduleName || strings.HasPrefix(p, moduleName+"/") {
			return true
		}
	}
	return false
}
