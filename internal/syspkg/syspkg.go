// Package syspkg installs packages via the host's native Linux package manager
// (apt or dnf/yum). On systems without one, the install functions are no-ops.
package syspkg

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/AugustDG/dotfiles/internal/runner"
)

func HasApt() bool {
	_, err := exec.LookPath("apt-get")
	return err == nil
}

func HasDnf() bool {
	if _, err := exec.LookPath("dnf"); err == nil {
		return true
	}
	_, err := exec.LookPath("yum")
	return err == nil
}

// IsAptInstalled reports whether a dpkg package is installed.
func IsAptInstalled(pkg string) bool {
	out, err := exec.Command("dpkg-query", "-W", "-f=${Status}", pkg).Output()
	return err == nil && strings.Contains(string(out), "install ok installed")
}

// IsDnfInstalled reports whether an rpm package is installed.
func IsDnfInstalled(pkg string) bool {
	return exec.Command("rpm", "-q", pkg).Run() == nil
}

func dnfBin() string {
	if _, err := exec.LookPath("dnf"); err == nil {
		return "dnf"
	}
	return "yum"
}

// InstallApt installs packages via apt-get. Empty list or missing apt is a no-op.
func InstallApt(pkgs []string) error {
	if len(pkgs) == 0 || !HasApt() {
		return nil
	}
	runner.Sudo("apt-get", "update").Run()
	args := append([]string{"install", "-y"}, pkgs...)
	if err := runner.Sudo("apt-get", args...).Run(); err != nil {
		return fmt.Errorf("apt-get install %s: %w", strings.Join(pkgs, " "), err)
	}
	return nil
}

// InstallDnf installs packages via dnf/yum. Empty list or missing dnf is a no-op.
func InstallDnf(pkgs []string) error {
	if len(pkgs) == 0 || !HasDnf() {
		return nil
	}
	args := append([]string{"install", "-y"}, pkgs...)
	if err := runner.Sudo(dnfBin(), args...).Run(); err != nil {
		return fmt.Errorf("%s install %s: %w", dnfBin(), strings.Join(pkgs, " "), err)
	}
	return nil
}

// InstallPrereqs installs the build prerequisites Homebrew needs on Linux.
func InstallPrereqs() error {
	if HasApt() {
		return InstallApt([]string{"build-essential", "procps", "curl", "file", "git"})
	}
	if HasDnf() {
		return InstallDnf([]string{"git", "curl", "procps-ng", "file", "gcc", "make"})
	}
	return nil
}
