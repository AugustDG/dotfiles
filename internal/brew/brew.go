package brew

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// IsInstalled returns true if the brew command is available on PATH.
func IsInstalled() bool {
	_, err := exec.LookPath("brew")
	return err == nil
}

// Install runs the official Homebrew install script non-interactively.
func Install() error {
	cmd := exec.Command("/bin/bash", "-c",
		`$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)`)
	cmd.Env = append(os.Environ(), "NONINTERACTIVE=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// EnsureOnPath checks common Homebrew prefixes and adds the first existing
// one to PATH if brew is not already reachable.
func EnsureOnPath() {
	if IsInstalled() {
		return
	}

	prefixes := []string{
		"/opt/homebrew",
		"/usr/local",
		"/home/linuxbrew/.linuxbrew",
	}

	for _, prefix := range prefixes {
		bin := prefix + "/bin"
		if _, err := os.Stat(bin + "/brew"); err == nil {
			os.Setenv("PATH", bin+":"+os.Getenv("PATH"))
			return
		}
	}
}

// IsPackageInstalled returns true if the given formula is installed via brew.
func IsPackageInstalled(pkg string) bool {
	cmd := exec.Command("brew", "list", "--formula", pkg)
	return cmd.Run() == nil
}

// InstallPackages installs the given brew formulae, skipping any that are
// already installed.
func InstallPackages(pkgs []string) error {
	var toInstall []string
	for _, pkg := range pkgs {
		if !IsPackageInstalled(pkg) {
			toInstall = append(toInstall, pkg)
		}
	}

	if len(toInstall) == 0 {
		return nil
	}

	args := append([]string{"install"}, toInstall...)
	cmd := exec.Command("brew", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("brew install %s: %w", strings.Join(toInstall, " "), err)
	}
	return nil
}
