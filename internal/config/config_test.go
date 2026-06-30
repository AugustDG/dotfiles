package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSupportsOS(t *testing.T) {
	cases := []struct {
		os   []string
		want bool
	}{
		{nil, true}, // no restriction => supported everywhere
		{[]string{"darwin"}, true},
		{[]string{"DARWIN"}, true}, // case-insensitive
		{[]string{"linux"}, false},
		{[]string{"linux", "darwin"}, true},
	}
	for _, c := range cases {
		m := Module{OS: c.os}
		if got := m.SupportsOS("darwin"); got != c.want {
			t.Errorf("SupportsOS(darwin) for %v = %v, want %v", c.os, got, c.want)
		}
	}
}

func TestDepsEmpty(t *testing.T) {
	if !(Deps{}).Empty() {
		t.Error("zero Deps should be empty")
	}
	if (Deps{Brew: []string{"x"}}).Empty() {
		t.Error("Deps with brew should not be empty")
	}
	if (Deps{Apt: []string{"y"}}).Empty() {
		t.Error("Deps with apt should not be empty")
	}
}

func TestParseGitmodules(t *testing.T) {
	dir := t.TempDir()
	content := `[submodule ".config/nvim"]
	path = nvim/.config/nvim
	url = git@github.com:x/nvim.git
[submodule "claude/skills"]
	path = claude/.claude/skills/greptile
	url = https://github.com/x/skills.git
`
	p := filepath.Join(dir, ".gitmodules")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := parseGitmodules(p)
	want := []string{"nvim/.config/nvim", "claude/.claude/skills/greptile"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("path %d: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestParseGitmodules_Missing(t *testing.T) {
	if got := parseGitmodules(filepath.Join(t.TempDir(), "nope")); got != nil {
		t.Errorf("missing file should yield nil, got %v", got)
	}
}

func TestMatchingSubmodulePaths(t *testing.T) {
	paths := []string{"nvim/.config/nvim", "tmux/.config/tmux", "claude/.claude/skills/greptile"}

	has, matches := matchingSubmodulePaths(paths, "nvim")
	if !has || len(matches) != 1 || matches[0] != "nvim/.config/nvim" {
		t.Errorf("nvim: has=%v matches=%v", has, matches)
	}

	has, matches = matchingSubmodulePaths(paths, "claude")
	if !has || len(matches) != 1 || matches[0] != "claude/.claude/skills/greptile" {
		t.Errorf("claude: has=%v matches=%v", has, matches)
	}

	if has, _ := matchingSubmodulePaths(paths, "git"); has {
		t.Error("git should have no submodules")
	}
}

func TestDiscoverModules(t *testing.T) {
	dir := t.TempDir()
	// A valid module.
	if err := os.MkdirAll(filepath.Join(dir, "zsh"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "zsh", "module.toml"),
		"name = \"zsh\"\ndescription = \"shell\"\nos = [\"darwin\", \"linux\"]\n[deps]\nbrew = [\"zsh\"]\n")
	mustWrite(t, filepath.Join(dir, "zsh", ".zshrc"), "x")

	// A plain directory without module.toml is ignored.
	if err := os.MkdirAll(filepath.Join(dir, "notamodule"), 0o755); err != nil {
		t.Fatal(err)
	}

	mods, err := DiscoverModules(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(mods) != 1 {
		t.Fatalf("expected 1 module, got %d (%v)", len(mods), mods)
	}
	m := mods[0]
	if m.Name != "zsh" || m.Description != "shell" {
		t.Errorf("unexpected module metadata: %+v", m)
	}
	if len(m.Deps.Brew) != 1 || m.Deps.Brew[0] != "zsh" {
		t.Errorf("expected brew dep zsh, got %v", m.Deps.Brew)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
