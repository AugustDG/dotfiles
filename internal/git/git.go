package git

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	} else if err != nil {
		return "", err
	}

	cmd := exec.Command("git", "-C", fullPath, "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git status %s: %w", path, err)
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
	pairs, err := submoduleConfigPairs(path)
	if err != nil {
		return nil
	}
	paths := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		paths = append(paths, pair.value)
	}
	return paths
}

type configPair struct {
	key   string
	value string
}

// submoduleConfigPairs uses NUL-delimited output because both valid section
// names and submodule paths may contain spaces.
func submoduleConfigPairs(repo string) ([]configPair, error) {
	cmd := exec.Command("git", "-C", repo, "config", "--file", ".gitmodules", "-z", "--get-regexp", `^submodule\..*\.path$`)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var pairs []configPair
	for _, record := range bytes.Split(out, []byte{0}) {
		key, value, ok := bytes.Cut(record, []byte{'\n'})
		if ok {
			pairs = append(pairs, configPair{key: string(key), value: string(value)})
		}
	}
	return pairs, nil
}

func IsDirty(path string) bool {
	out, err := exec.Command("git", "-C", path, "status", "--porcelain").Output()
	return err == nil && len(strings.TrimSpace(string(out))) > 0
}

// PathHasChanges reports whether a repo-relative path has staged, unstaged, or
// untracked changes. For submodules this includes a changed gitlink or dirty
// nested working tree.
func PathHasChanges(repo, path string) bool {
	out, err := exec.Command("git", "-C", repo, "status", "--porcelain", "--", path).Output()
	return err != nil || len(strings.TrimSpace(string(out))) > 0
}

// SubmoduleRemoval records enough superproject state to restore submodule
// registrations and index gitlinks if removing the containing module fails.
// The cached repositories under .git/modules are intentionally retained.
type SubmoduleRemoval struct {
	Paths          []string
	Sections       []string
	Gitlinks       map[string]string
	Gitmodules     []byte
	GitmodulesMode os.FileMode
}

// ValidateSubmoduleRegistrations verifies that every path has both an
// authoritative .gitmodules section and a mode-160000 gitlink in the index.
func ValidateSubmoduleRegistrations(repo string, paths []string) error {
	_, err := submoduleRemovalMetadata(repo, paths)
	return err
}

// PrepareSubmoduleRemoval deinitializes the requested submodules, removes their
// .gitmodules sections, and stages removal of their gitlinks. This keeps the
// superproject index coherent while the containing module directory is absent.
func PrepareSubmoduleRemoval(repo string, paths []string) (*SubmoduleRemoval, error) {
	snapshot, err := submoduleRemovalMetadata(repo, paths)
	if err != nil {
		return nil, err
	}

	args := append([]string{"-C", repo, "submodule", "deinit", "-f", "--"}, paths...)
	if err := runGit(args...); err != nil {
		return nil, fmt.Errorf("deinitialize submodules: %w", err)
	}

	fail := func(cause error) (*SubmoduleRemoval, error) {
		if restoreErr := snapshot.Restore(repo); restoreErr != nil {
			return nil, fmt.Errorf("%w (also failed to restore submodules: %v)", cause, restoreErr)
		}
		return nil, cause
	}
	for _, section := range snapshot.Sections {
		if err := runGit("-C", repo, "config", "--file", ".gitmodules", "--remove-section", section); err != nil {
			return fail(fmt.Errorf("remove %s from .gitmodules: %w", section, err))
		}
	}
	for _, path := range snapshot.Paths {
		if err := runGit("-C", repo, "update-index", "--force-remove", "--", path); err != nil {
			return fail(fmt.Errorf("remove gitlink %s: %w", path, err))
		}
	}
	return snapshot, nil
}

// Restore reverses PrepareSubmoduleRemoval and reinitializes the worktrees from
// the retained repositories under .git/modules.
func (s *SubmoduleRemoval) Restore(repo string) error {
	gitmodulesPath := filepath.Join(repo, ".gitmodules")
	if err := os.WriteFile(gitmodulesPath, s.Gitmodules, s.GitmodulesMode.Perm()); err != nil {
		return err
	}
	if err := os.Chmod(gitmodulesPath, s.GitmodulesMode.Perm()); err != nil {
		return err
	}
	for _, path := range s.Paths {
		cacheInfo := fmt.Sprintf("160000,%s,%s", s.Gitlinks[path], path)
		if err := runGit("-C", repo, "update-index", "--add", "--cacheinfo", cacheInfo); err != nil {
			return err
		}
	}
	args := append([]string{"-C", repo, "submodule", "update", "--init", "--recursive", "--"}, s.Paths...)
	return runGit(args...)
}

func submoduleRemovalMetadata(repo string, paths []string) (*SubmoduleRemoval, error) {
	gitmodulesPath := filepath.Join(repo, ".gitmodules")
	contents, err := os.ReadFile(gitmodulesPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(gitmodulesPath)
	if err != nil {
		return nil, err
	}

	pairs, err := submoduleConfigPairs(repo)
	if err != nil {
		return nil, fmt.Errorf("read .gitmodules: %w", err)
	}
	sectionsByPath := make(map[string]string)
	for _, pair := range pairs {
		sectionsByPath[pair.value] = strings.TrimSuffix(pair.key, ".path")
	}

	snapshot := &SubmoduleRemoval{
		Paths:          append([]string(nil), paths...),
		Gitlinks:       make(map[string]string, len(paths)),
		Gitmodules:     contents,
		GitmodulesMode: info.Mode(),
	}
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		if seen[path] {
			return nil, fmt.Errorf("duplicate submodule path: %s", path)
		}
		seen[path] = true
		section, ok := sectionsByPath[path]
		if !ok {
			return nil, fmt.Errorf("no .gitmodules registration found for %s", path)
		}
		snapshot.Sections = append(snapshot.Sections, section)

		indexOut, err := exec.Command("git", "-C", repo, "ls-files", "--stage", "--", path).Output()
		if err != nil {
			return nil, err
		}
		fields := strings.Fields(strings.TrimSpace(string(indexOut)))
		if len(fields) < 3 || fields[0] != "160000" || fields[2] != "0" {
			return nil, fmt.Errorf("no submodule gitlink found in index for %s", path)
		}
		snapshot.Gitlinks[path] = fields[1]
	}
	return snapshot, nil
}

// CurrentBranch returns the checked-out branch, or an error on detached HEAD.
func CurrentBranch(path string) (string, error) {
	out, err := exec.Command("git", "-C", path, "symbolic-ref", "--quiet", "--short", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("detached HEAD")
	}
	return strings.TrimSpace(string(out)), nil
}

// AttachableBranch returns a local branch that can host commits made at the
// current (detached) HEAD without losing history — i.e. a branch that is an
// ancestor of, or equal to, HEAD, so re-pointing it at HEAD is a fast-forward.
// This is the common state of a freshly-updated submodule (detached at the
// pinned commit, with a possibly-stale local branch behind it). Prefers
// main/master. ok is false when HEAD diverges from every local branch, in
// which case the caller should refuse to guess rather than risk losing commits.
func AttachableBranch(path string) (string, bool) {
	out, err := exec.Command("git", "-C", path, "for-each-ref", "--format=%(refname:short)", "refs/heads").Output()
	if err != nil {
		return "", false
	}
	var candidates []string
	for _, line := range strings.Split(string(out), "\n") {
		b := strings.TrimSpace(line)
		if b == "" {
			continue
		}
		// Only branches that are an ancestor of (or equal to) HEAD are safe to
		// fast-forward onto HEAD; an ahead/divergent branch would lose commits.
		if exec.Command("git", "-C", path, "merge-base", "--is-ancestor", b, "HEAD").Run() == nil {
			candidates = append(candidates, b)
		}
	}
	if len(candidates) == 0 {
		return "", false
	}
	for _, b := range candidates {
		if b == "main" || b == "master" {
			return b, true
		}
	}
	return candidates[0], true
}

// AttachBranch points branch at the current HEAD (a fast-forward when the
// branch is behind) and checks it out, preserving any working-tree changes.
// Callers must have verified the branch is an ancestor of HEAD (see
// AttachableBranch) so the reset never discards commits.
func AttachBranch(path, branch string) error {
	return runGit("-C", path, "checkout", "-B", branch)
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

// StagedPaths returns the repo-relative paths staged for commit (index vs HEAD)
// at path. Used to summarise what a commit will contain. Empty on error or when
// nothing is staged.
func StagedPaths(path string) []string {
	out, err := exec.Command("git", "-C", path, "diff", "--cached", "--name-only").Output()
	if err != nil {
		return nil
	}
	return splitLines(string(out))
}

// ChangedPaths returns the repo-relative paths with any pending change — staged,
// unstaged, or untracked — at path, i.e. what `git add -A` would stage. Used to
// preview a commit message before staging (e.g. in a dry run).
func ChangedPaths(path string) []string {
	out, err := exec.Command("git", "-C", path, "status", "--porcelain").Output()
	if err != nil {
		return nil
	}
	var paths []string
	for _, l := range splitLines(string(out)) {
		if len(l) < 4 {
			continue
		}
		// Porcelain v1 lines are "XY <path>"; renames are "<orig> -> <new>".
		p := strings.TrimSpace(l[3:])
		if i := strings.Index(p, " -> "); i >= 0 {
			p = p[i+4:]
		}
		paths = append(paths, strings.Trim(p, `"`))
	}
	return paths
}

func splitLines(s string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
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
