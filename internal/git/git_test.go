package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSSHToHTTPS(t *testing.T) {
	cases := map[string]string{
		"git@github.com:AugustDG/dotfiles.git": "https://github.com/AugustDG/dotfiles.git",
		"git@gitlab.com:group/sub/repo.git":    "https://gitlab.com/group/sub/repo.git",
	}
	for in, want := range cases {
		if got := sshToHTTPS(in); got != want {
			t.Errorf("sshToHTTPS(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAtoi(t *testing.T) {
	cases := map[string]int{"0": 0, "5": 5, "42": 42, "": 0, "12x": 12, "x": 0}
	for in, want := range cases {
		if got := atoi(in); got != want {
			t.Errorf("atoi(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestPrepareSubmoduleRemovalAndRestore(t *testing.T) {
	repo, submodulePath := setupSubmoduleRepo(t)
	originalGitmodules, err := os.ReadFile(filepath.Join(repo, ".gitmodules"))
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := PrepareSubmoduleRemoval(repo, []string{submodulePath})
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(repo, ".gitmodules"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), submodulePath) {
		t.Fatalf("submodule registration remains:\n%s", contents)
	}
	if out, _ := exec.Command("git", "-C", repo, "ls-files", "--stage", "--", submodulePath).Output(); len(out) != 0 {
		t.Fatalf("gitlink remains in index: %s", out)
	}

	if err := snapshot.Restore(repo); err != nil {
		t.Fatal(err)
	}
	restoredGitmodules, err := os.ReadFile(filepath.Join(repo, ".gitmodules"))
	if err != nil {
		t.Fatal(err)
	}
	if string(restoredGitmodules) != string(originalGitmodules) {
		t.Fatalf(".gitmodules was not restored:\n%s", restoredGitmodules)
	}
	if _, err := os.Stat(filepath.Join(repo, submodulePath, "sub.conf")); err != nil {
		t.Fatalf("submodule worktree was not restored: %v", err)
	}
	if out, err := exec.Command("git", "-C", repo, "status", "--porcelain").Output(); err != nil || len(out) != 0 {
		t.Fatalf("repo not clean after restore: %v: %s", err, out)
	}
}

func TestValidateSubmoduleRegistrationsBeforeMutation(t *testing.T) {
	repo, submodulePath := setupSubmoduleRepo(t)
	gitmodulesPath := filepath.Join(repo, ".gitmodules")
	original, err := os.ReadFile(gitmodulesPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := ValidateSubmoduleRegistrations(repo, []string{submodulePath, "missing"}); err == nil {
		t.Fatal("expected missing-registration error")
	}
	contents, err := os.ReadFile(gitmodulesPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != string(original) {
		t.Fatalf(".gitmodules changed after validation failure:\n%s", contents)
	}
}

func setupSubmoduleRepo(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	subRepo := filepath.Join(root, "sub")
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(subRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, subRepo, "init", "-q")
	runGitCommand(t, subRepo, "config", "user.email", "test@example.com")
	runGitCommand(t, subRepo, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(subRepo, "sub.conf"), []byte("submodule"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, subRepo, "add", ".")
	runGitCommand(t, subRepo, "commit", "-qm", "init")

	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, repo, "init", "-q")
	runGitCommand(t, repo, "config", "user.email", "test@example.com")
	runGitCommand(t, repo, "config", "user.name", "Test")
	submodulePath := "app/sub module"
	cmd := exec.Command("git", "-C", repo, "-c", "protocol.file.allow=always", "submodule", "add", "-q", subRepo, submodulePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("add submodule: %v: %s", err, out)
	}
	runGitCommand(t, repo, "add", ".")
	runGitCommand(t, repo, "commit", "-qm", "init")
	return repo, submodulePath
}

func runGitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}
