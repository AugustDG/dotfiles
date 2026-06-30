package stow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsStowed_AbsoluteAndRelativeLinks(t *testing.T) {
	repo, home := setup(t)
	writeFile(t, filepath.Join(repo, "m", "module.toml"), "name='m'")
	src := filepath.Join(repo, "m", ".zshrc")
	writeFile(t, src, "x")

	// Not linked yet.
	if IsStowed(repo, "m", home) {
		t.Fatal("expected not stowed before linking")
	}

	// Absolute symlink, as stow-like tools may create.
	abs, _ := filepath.Abs(src)
	symlink(t, abs, filepath.Join(home, ".zshrc"))
	if !IsStowed(repo, "m", home) {
		t.Fatal("expected stowed via absolute symlink")
	}
}

func TestIsStowed_DirectorySymlink(t *testing.T) {
	repo, home := setup(t)
	// A module whose whole subtree is owned via a single directory symlink.
	writeFile(t, filepath.Join(repo, "m", "module.toml"), "name='m'")
	writeFile(t, filepath.Join(repo, "m", ".config", "app", "a.conf"), "1")
	writeFile(t, filepath.Join(repo, "m", ".config", "app", "b.conf"), "2")

	// Link ~/.config/app -> repo/m/.config/app (directory symlink, "folding").
	src := filepath.Join(repo, "m", ".config", "app")
	symlink(t, src, filepath.Join(home, ".config", "app"))

	if !IsStowed(repo, "m", home) {
		t.Fatal("expected stowed via directory symlink ancestor")
	}
}

func TestIsStowed_PartialIsNotStowed(t *testing.T) {
	repo, home := setup(t)
	writeFile(t, filepath.Join(repo, "m", "a"), "1")
	writeFile(t, filepath.Join(repo, "m", "b"), "2")
	symlink(t, filepath.Join(repo, "m", "a"), filepath.Join(home, "a"))
	// "b" intentionally not linked.

	if IsStowed(repo, "m", home) {
		t.Fatal("partially-linked module should not report stowed")
	}
}

func TestIsStowed_WrongTargetIsNotStowed(t *testing.T) {
	repo, home := setup(t)
	writeFile(t, filepath.Join(repo, "m", ".zshrc"), "x")
	// A symlink that points somewhere else entirely.
	other := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.WriteFile(other, []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	symlink(t, other, filepath.Join(home, ".zshrc"))

	if IsStowed(repo, "m", home) {
		t.Fatal("symlink to a different target should not report stowed")
	}
}
