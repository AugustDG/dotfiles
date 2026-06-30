package git

import (
	"bytes"
	"fmt"
	"io"
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

// Pull fast-forwards the repo at path from its upstream.
func Pull(path string) error {
	return runGit("-C", path, "pull", "--ff-only")
}

func PullSubmodule(path string) error {
	return Pull(path)
}

// SyncSubmodules initialises and updates every submodule in the repo at path to
// the commit recorded by the superproject.
func SyncSubmodules(path string) error {
	return runGit("-C", path, "submodule", "update", "--init", "--recursive")
}

// HasUpstream reports whether the current branch has a configured upstream.
func HasUpstream(path string) bool {
	return exec.Command("git", "-C", path,
		"rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}").Run() == nil
}

// AheadBehind returns how many commits HEAD is ahead of and behind its
// upstream. Both are 0 when there is no upstream.
func AheadBehind(path string) (ahead, behind int) {
	out, err := exec.Command("git", "-C", path,
		"rev-list", "--left-right", "--count", "@{upstream}...HEAD").Output()
	if err != nil {
		return 0, 0
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) != 2 {
		return 0, 0
	}
	behind = atoi(fields[0])
	ahead = atoi(fields[1])
	return ahead, behind
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// Submodules returns the relative paths of submodules declared in the
// .gitmodules file of the repo at path.
func Submodules(path string) []string {
	cmd := exec.Command("git", "-C", path, "config",
		"--file", ".gitmodules", "--get-regexp", `submodule\..*\.path`)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var paths []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 {
			paths = append(paths, parts[1])
		}
	}
	return paths
}

func IsDirty(path string) bool {
	out, err := exec.Command("git", "-C", path, "status", "--porcelain").Output()
	return err == nil && len(strings.TrimSpace(string(out))) > 0
}

// CurrentBranch returns the checked-out branch, or an error on detached HEAD.
func CurrentBranch(path string) (string, error) {
	out, err := exec.Command("git", "-C", path, "symbolic-ref", "--quiet", "--short", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("detached HEAD")
	}
	return strings.TrimSpace(string(out)), nil
}

// HasUnpushed reports whether HEAD has commits not on its upstream. Returns
// false when there is no upstream (e.g. detached HEAD in a submodule).
func HasUnpushed(path string) bool {
	out, err := exec.Command("git", "-C", path, "rev-list", "--count", "@{upstream}..HEAD").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != "0"
}

func Add(path string, specs ...string) error {
	return runGit(append([]string{"-C", path, "add"}, specs...)...)
}

// HasStaged reports whether the index at path differs from HEAD.
func HasStaged(path string) bool {
	return exec.Command("git", "-C", path, "diff", "--cached", "--quiet").Run() != nil
}

func Commit(path, message string) error {
	return runGit("-C", path, "commit", "-m", message)
}

func Push(path string) error {
	return runGit("-C", path, "push")
}

// runGit runs a git command, surfacing stderr (minus hint lines) as the error.
func runGit(args ...string) error {
	cmd := exec.Command("git", args...)
	var stderr bytes.Buffer
	if runner.Verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	} else {
		cmd.Stdout = io.Discard
		cmd.Stderr = &stderr
	}
	err := cmd.Run()
	if err == nil {
		return nil
	}

	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(stderr.String()), "\n") {
		if !strings.HasPrefix(l, "hint:") {
			lines = append(lines, l)
		}
	}
	if len(lines) > 0 {
		return fmt.Errorf("%s", strings.Join(lines, "\n"))
	}
	return err
}

func sshToHTTPS(sshURL string) string {
	s := strings.TrimPrefix(sshURL, "git@")
	s = strings.Replace(s, ":", "/", 1)
	return "https://" + s
}
