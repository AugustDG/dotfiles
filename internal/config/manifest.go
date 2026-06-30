package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// ManifestFile is the name of the optional top-level configuration file.
const ManifestFile = "dotfiles.toml"

// Manifest is the optional top-level dotfiles.toml that customises the
// bootstrap toolchain. When absent, built-in defaults are used.
type Manifest struct {
	Bootstrap BootstrapConfig `toml:"bootstrap"`
}

// BootstrapConfig declares the packages and files the bootstrap phase manages.
type BootstrapConfig struct {
	// CoreBrew is installed before the repo is cloned (git/stow/gh/zsh).
	CoreBrew []string `toml:"core_brew"`
	// GlobalBrew is installed after the repo is cloned (general toolchain).
	GlobalBrew []string `toml:"global_brew"`
	// BackupTargets are files in $HOME moved aside if they exist as real
	// files (not symlinks) before stowing.
	BackupTargets []string `toml:"backup_targets"`
}

// ManifestPath returns the expected location of the manifest within dotfilesDir.
func ManifestPath(dotfilesDir string) string {
	return filepath.Join(dotfilesDir, ManifestFile)
}

// LoadManifest reads dotfiles.toml from dotfilesDir. A missing file is not an
// error: it returns a zero Manifest so callers fall back to built-in defaults.
func LoadManifest(dotfilesDir string) (Manifest, error) {
	var m Manifest
	path := ManifestPath(dotfilesDir)
	if _, err := os.Stat(path); err != nil {
		return m, nil
	}
	if _, err := toml.DecodeFile(path, &m); err != nil {
		return m, err
	}
	return m, nil
}
