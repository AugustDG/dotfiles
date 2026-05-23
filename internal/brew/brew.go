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
	runner.ConfigureCmd(cmd)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("brew install %s: %w", strings.Join(toInstall, " "), err)
	}
	return nil
}
