package git

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/AugustDG/dotfiles/internal/runner"
)

const (
	RepoSSH   = "git@github.com:AugustDG/dotfiles.git"
	RepoHTTPS = "https://github.com/AugustDG/dotfiles.git"
)

func IsGHAuthenticated() bool {
	cmd := exec.Command("gh", "auth", "status", "--hostname", "github.com")
	return cmd.Run() == nil
}

func GHAuthLogin() error {
	login := exec.Command("gh", "auth", "login",
		"--hostname", "github.com",
		"--git-protocol", "ssh",
		"--web")
	tty, err := os.Open("/dev/tty")
	if err != nil {
		login.Stdin = os.Stdin
	} else {
		login.Stdin = tty
		defer tty.Close()
	}
	login.Stdout = os.Stdout
	login.Stderr = os.Stderr

	if err := login.Run(); err != nil {
		return fmt.Errorf("gh auth login: %w", err)
	}

	setup := exec.Command("gh", "auth", "setup-git")
	runner.ConfigureCmd(setup)
	if err := setup.Run(); err != nil {
		return fmt.Errorf("gh auth setup-git: %w", err)
	}

	return nil
}

func CloneRepo(url, dest string) error {
	cmd := exec.Command("git", "clone", "--recurse-submodules", url, dest)
	runner.ConfigureCmd(cmd)
	if err := cmd.Run(); err != nil {
		if strings.HasPrefix(url, "git@") {
			httpsURL := sshToHTTPS(url)
			fallback := exec.Command("git", "clone", "--recurse-submodules", httpsURL, dest)
			runner.ConfigureCmd(fallback)
			return fallback.Run()
		}
		return err
	}
	return nil
}

func InitSubmodules(dotfilesDir string, modulePaths []string) error {
	for _, p := range modulePaths {
		cmd := exec.Command("git", "-C", dotfilesDir,
			"submodule", "update", "--init", "--recursive", "--", p)
		runner.ConfigureCmd(cmd)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("submodule init %s: %w", p, err)
		}
	}
	return nil
}

func SubmoduleStatus(dotfilesDir, path string) (string, error) {
	fullPath := path
	if !strings.HasPrefix(path, "/") {
		fullPath = dotfilesDir + "/" + path
	}

	if _, err := os.Stat(fullPath + "/.git"); os.IsNotExist(err) {
		return "not-init", nil
	}

	cmd := exec.Command("git", "-C", fullPath, "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return "not-init", nil
	}

	if len(strings.TrimSpace(string(out))) == 0 {
		return "clean", nil
	}
	return "dirty", nil
}

func PullSubmodule(path string) error {
	cmd := exec.Command("git", "-C", path, "pull", "--ff-only")
	runner.ConfigureCmd(cmd)
	return cmd.Run()
}

func sshToHTTPS(sshURL string) string {
	s := strings.TrimPrefix(sshURL, "git@")
	s = strings.Replace(s, ":", "/", 1)
	return "https://" + s
}
