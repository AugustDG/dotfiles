package bootstrap

import (
	"runtime"

	"github.com/AugustDG/dotfiles/internal/brew"
	"github.com/AugustDG/dotfiles/internal/config"
	"github.com/AugustDG/dotfiles/internal/syspkg"
)

// DepNames returns the dependency package names relevant to the current OS,
// for display in progress output.
func DepNames(d config.Deps) []string {
	names := append([]string{}, d.Brew...)
	if runtime.GOOS == "darwin" {
		names = append(names, d.Cask...)
	}
	if runtime.GOOS == "linux" {
		if syspkg.HasApt() {
			names = append(names, d.Apt...)
		}
		if syspkg.HasDnf() {
			names = append(names, d.Dnf...)
		}
	}
	return names
}

// MissingDeps returns the OS-relevant dependency names for a module that are
// not currently installed. The formulae and casks sets (from
// brew.InstalledFormulae/InstalledCasks) are membership-tested in memory;
// apt/dnf state is queried per package.
func MissingDeps(d config.Deps, formulae, casks map[string]bool) []string {
	var missing []string
	for _, p := range d.Brew {
		if !formulae[brew.PackageLeaf(p)] {
			missing = append(missing, p)
		}
	}
	if runtime.GOOS == "darwin" {
		for _, c := range d.Cask {
			if !casks[brew.PackageLeaf(c)] {
				missing = append(missing, c)
			}
		}
	}
	if runtime.GOOS == "linux" {
		for _, p := range d.Apt {
			if syspkg.HasApt() && !syspkg.IsAptInstalled(p) {
				missing = append(missing, p)
			}
		}
		for _, p := range d.Dnf {
			if syspkg.HasDnf() && !syspkg.IsDnfInstalled(p) {
				missing = append(missing, p)
			}
		}
	}
	return missing
}

// InstallDeps installs a module's dependencies using the package managers
// appropriate for the current OS: brew everywhere, casks on macOS, and
// apt/dnf on Linux.
func InstallDeps(d config.Deps) error {
	if err := brew.InstallPackages(d.Brew); err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		if err := brew.InstallCasks(d.Cask); err != nil {
			return err
		}
	}
	if runtime.GOOS == "linux" {
		if err := syspkg.InstallApt(d.Apt); err != nil {
			return err
		}
		if err := syspkg.InstallDnf(d.Dnf); err != nil {
			return err
		}
	}
	return nil
}
