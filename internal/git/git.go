package git

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	// RepoSSH is the SSH URL for the dotfiles repository.
	RepoSSH = "git@github.com:AugustDG/dotfiles.git"
	// RepoHTTPS is the HTTPS URL for the dotfiles repository.
	RepoHTTPS = "https://github.com/AugustDG/dotfiles.git"
)

// IsGHAuthenticated returns true if the GitHub CLI is authenticated.
func IsGHAuthenticated() bool {
	cmd := exec.Command("gh", "auth", "status", "--hostname", "github.com")
	return cmd.Run() == nil
}

// GHAuthLogin performs interactive GitHub CLI authentication via web browser
// and configures git credential helper.
func GHAuthLogin() error {
	login := exec.Command("gh", "auth", "login",
		"--hostname", "github.com",
		"--git-protocol", "ssh",
		"--web")
	login.Stdin = os.Stdin
	login.Stdout = os.Stdout
	login.Stderr = os.Stderr

	if err := login.Run(); err != nil {
		return fmt.Errorf("gh auth login: %w", err)
	}

	setup := exec.Command("gh", "auth", "setup-git")
	setup.Stdout = os.Stdout
	setup.Stderr = os.Stderr

	if err := setup.Run(); err != nil {
		return fmt.Errorf("gh auth setup-git: %w", err)
	}

	return nil
}

// CloneRepo clones the given repository to dest with submodules. Falls back
// to HTTPS if the SSH URL fails.
func CloneRepo(url, dest string) error {
	cmd := exec.Command("git", "clone", "--recurse-submodules", url, dest)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		// If the URL looks like SSH, retry with HTTPS.
		if strings.HasPrefix(url, "git@") {
			httpsURL := sshToHTTPS(url)
			fallback := exec.Command("git", "clone", "--recurse-submodules", httpsURL, dest)
			fallback.Stdout = os.Stdout
			fallback.Stderr = os.Stderr
			return fallback.Run()
		}
		return err
	}
	return nil
}

// InitSubmodules initializes and updates specific submodule paths.
func InitSubmodules(dotfilesDir string, modulePaths []string) error {
	for _, p := range modulePaths {
		cmd := exec.Command("git", "-C", dotfilesDir,
			"submodule", "update", "--init", "--recursive", "--", p)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("submodule init %s: %w", p, err)
		}
	}
	return nil
}

// SubmoduleStatus returns the working tree status for a submodule path:
// "clean", "dirty", or "not-init".
func SubmoduleStatus(dotfilesDir, path string) (string, error) {
	fullPath := path
	if !strings.HasPrefix(path, "/") {
		fullPath = dotfilesDir + "/" + path
	}

	// Check if the directory exists and has a .git file/dir.
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

// PullSubmodule performs a fast-forward pull in the given submodule path.
func PullSubmodule(path string) error {
	cmd := exec.Command("git", "-C", path, "pull", "--ff-only")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// sshToHTTPS converts a git SSH URL to its HTTPS equivalent.
func sshToHTTPS(sshURL string) string {
	// git@github.com:user/repo.git -> https://github.com/user/repo.git
	s := strings.TrimPrefix(sshURL, "git@")
	s = strings.Replace(s, ":", "/", 1)
	return "https://" + s
}
