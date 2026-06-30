package stow

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// setup builds a repo/<module> tree and an empty home dir, returning their
// absolute paths.
func setup(t *testing.T) (repo, home string) {
	t.Helper()
	root := t.TempDir()
	repo = filepath.Join(root, "repo")
	home = filepath.Join(root, "home")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	return repo, home
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func TestModuleLeaves(t *testing.T) {
	repo, _ := setup(t)
	writeFile(t, filepath.Join(repo, "m", "module.toml"), "name='m'")
	writeFile(t, filepath.Join(repo, "m", ".zshrc"), "x")
	writeFile(t, filepath.Join(repo, "m", ".config", "foo", "bar.conf"), "y")
	writeFile(t, filepath.Join(repo, "m", "README.md"), "skip me")

	got := ModuleLeaves(repo, "m")
	sort.Strings(got)
	want := []string{".config/foo/bar.conf", ".zshrc"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if filepath.ToSlash(got[i]) != want[i] {
			t.Errorf("leaf %d: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestWithin(t *testing.T) {
	dir := "/home/user/repo"
	cases := []struct {
		path string
		want bool
	}{
		{"/home/user/repo", true},
		{"/home/user/repo/a/b", true},
		{"/home/user/repos", false},
		{"/home/user", false},
		{"/etc/passwd", false},
	}
	for _, c := range cases {
		if got := within(c.path, dir); got != c.want {
			t.Errorf("within(%q, %q) = %v, want %v", c.path, dir, got, c.want)
		}
	}
}

func TestFindDangling_TopLevelAndNested(t *testing.T) {
	repo, home := setup(t)

	// A healthy link (source exists) must be ignored.
	healthySrc := filepath.Join(repo, "m", ".gitconfig")
	writeFile(t, healthySrc, "ok")
	symlink(t, healthySrc, filepath.Join(home, ".gitconfig"))

	// A top-level dangling link (source removed).
	symlink(t, filepath.Join(repo, "m", ".toprc"), filepath.Join(home, ".toprc"))

	// A nested dangling link under ~/.config (source removed). This is the
	// regression case: the repo no longer has any file here, so the directory
	// is not a module-derived root, yet it must still be found.
	symlink(t, filepath.Join(repo, "m", ".config", "foo", "bar.conf"),
		filepath.Join(home, ".config", "foo", "bar.conf"))

	// A symlink pointing outside the repo must be ignored even when broken.
	symlink(t, filepath.Join(home, "nonexistent-elsewhere"), filepath.Join(home, ".other"))

	roots := ScanRoots(repo, home, []string{"m"})
	found := FindDangling(repo, home, roots)

	got := map[string]bool{}
	for _, l := range found {
		rel, _ := filepath.Rel(home, l.Path)
		got[filepath.ToSlash(rel)] = true
	}
	if len(got) != 2 || !got[".toprc"] || !got[".config/foo/bar.conf"] {
		t.Fatalf("dangling = %v; want exactly .toprc and .config/foo/bar.conf", got)
	}
}

func TestScanRoots_HeavyTopLevelBounded(t *testing.T) {
	repo, home := setup(t)
	// ghostty maps into ~/Library (heavy); claude into ~/.claude (normal).
	writeFile(t, filepath.Join(repo, "ghostty", "Library", "Application Support", "com.x", "config"), "1")
	writeFile(t, filepath.Join(repo, "claude", ".claude", "CLAUDE.md"), "2")

	set := map[string]bool{}
	for _, r := range ScanRoots(repo, home, []string{"ghostty", "claude"}) {
		set[r] = true
	}

	if set[filepath.Join(home, "Library")] {
		t.Error("must not add ~/Library as a recursive root (would walk millions of files)")
	}
	if !set[filepath.Join(home, "Library", "Application Support", "com.x")] {
		t.Error("expected the ghostty leaf's exact directory as a bounded root")
	}
	if !set[filepath.Join(home, ".claude")] {
		t.Error("expected ~/.claude top-level root for a non-heavy module")
	}
	if !set[filepath.Join(home, ".config")] {
		t.Error("~/.config must always be a root")
	}
}

func TestScanRoots_HeavyLeafDirectlyUnder(t *testing.T) {
	repo, home := setup(t)
	// A leaf sitting directly under a heavy top level must not cause the whole
	// heavy tree to be added as a recursive root.
	writeFile(t, filepath.Join(repo, "x", "Library", "somefile"), "1")
	for _, r := range ScanRoots(repo, home, []string{"x"}) {
		if r == filepath.Join(home, "Library") {
			t.Fatalf("must not add ~/Library wholesale for a leaf directly under it; roots=%v",
				ScanRoots(repo, home, []string{"x"}))
		}
	}
}

func TestFindDangling_None(t *testing.T) {
	repo, home := setup(t)
	src := filepath.Join(repo, "m", ".zshrc")
	writeFile(t, src, "x")
	symlink(t, src, filepath.Join(home, ".zshrc"))

	found := FindDangling(repo, home, ScanRoots(repo, home, []string{"m"}))
	if len(found) != 0 {
		t.Fatalf("expected no dangling links, got %v", found)
	}
}

func TestRemoveDangling(t *testing.T) {
	repo, home := setup(t)
	link := filepath.Join(home, ".toprc")
	symlink(t, filepath.Join(repo, "m", ".toprc"), link)

	removed, err := RemoveDangling([]DanglingLink{{Path: link}})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != link {
		t.Fatalf("removed = %v", removed)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf("link still present after removal")
	}
}
