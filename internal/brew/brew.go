package brew

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/AugustDG/dotfiles/internal/runner"
)

func IsInstalled() bool {
	_, err := exec.LookPath("brew")
	return err == nil
}

func Install() error {
	cmd := exec.Command("/bin/bash", "-c",
		`curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh | /bin/bash`)
	cmd.Env = append(os.Environ(), "NONINTERACTIVE=1")
	runner.ConfigureCmd(cmd)
	return cmd.Run()
}

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

func IsPackageInstalled(pkg string) bool {
	cmd := exec.Command("brew", "list", "--formula", pkg)
	return cmd.Run() == nil
}

func IsCaskInstalled(cask string) bool {
	cmd := exec.Command("brew", "list", "--cask", cask)
	return cmd.Run() == nil
}

func InstallPackages(pkgs []string) error {
	return install(nil, pkgs, "")
}

// InstallCasks installs the given casks, skipping any already present.
func InstallCasks(casks []string) error {
	return install([]string{"--cask"}, casks, "--cask ")
}

func install(flags, pkgs []string, logPrefix string) error {
	var toInstall []string
	for _, pkg := range pkgs {
		installed := IsPackageInstalled(pkg)
		if logPrefix != "" {
			installed = IsCaskInstalled(pkg)
		}
		if !installed {
			toInstall = append(toInstall, pkg)
		}
	}
	if len(toInstall) == 0 {
		return nil
	}

	args := append(append([]string{"install"}, flags...), toInstall...)
	cmd := exec.Command("brew", args...)
	runner.ConfigureCmd(cmd)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("brew install %s%s: %w", logPrefix, strings.Join(toInstall, " "), err)
	}
	return nil
}

// listNames runs `brew list <kind> -1` and returns the names as a set. A failure
// (e.g. brew not installed) yields an empty set rather than an error so callers
// can degrade gracefully.
func listNames(kind string) map[string]bool {
	set := make(map[string]bool)
	out, err := exec.Command("brew", "list", kind, "-1").Output()
	if err != nil {
		return set
	}
	for _, line := range strings.Fields(string(out)) {
		set[line] = true
	}
	return set
}

// InstalledFormulae returns the set of installed top-level brew formulae.
// Computing this once and membership-testing in memory avoids spawning a
// `brew list` per package when checking many dependencies.
func InstalledFormulae() map[string]bool {
	return listNames("--formula")
}

// InstalledCasks returns the set of installed brew casks.
func InstalledCasks() map[string]bool {
	return listNames("--cask")
}

// PackageLeaf returns the bare package name used by `brew list` for a possibly
// tap-qualified formula spec (e.g. "oven-sh/bun/bun" -> "bun").
func PackageLeaf(spec string) string {
	if i := strings.LastIndex(spec, "/"); i >= 0 {
		return spec[i+1:]
	}
	return spec
}
