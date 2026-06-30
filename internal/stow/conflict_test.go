package stow

import (
	"strings"
	"testing"
)

func TestParseConflictTargets(t *testing.T) {
	out := `WARNING! stowing tmux would cause conflicts:
  * cannot stow projects/dotfiles/tmux/.config/tmux/tmux.conf over existing target .config/tmux/tmux.conf since neither a link nor a directory and --adopt not specified
  * cannot stow projects/dotfiles/tmux/.config/tmux/plugins/tpm/tpm over existing target .config/tmux/plugins/tpm/tpm since neither a link nor a directory and --adopt not specified
  * existing target is not owned by stow: .config/tmux/plugins/foo/README.md
  * existing target is neither a link nor a directory: .zshrc
All operations aborted.`

	got := parseConflictTargets(out)
	want := []string{
		".config/tmux/tmux.conf",
		".config/tmux/plugins/tpm/tpm",
		".config/tmux/plugins/foo/README.md",
		".zshrc",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d targets %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("target %d: got %q want %q", i, got[i], want[i])
		}
	}
}

// TestParseConflictTargets_DirNonDirPhrasings covers GNU stow's directory vs
// non-directory collision messages, which lack the " since " clause and don't
// contain the generic "over existing target " substring.
func TestParseConflictTargets_DirNonDirPhrasings(t *testing.T) {
	out := `WARNING! stowing foo would cause conflicts:
  * cannot stow non-directory ../repo/foo/.config/foo over existing directory target .config/foo
  * cannot stow directory ../repo/bar/.config/bar over existing non-directory target .config/bar
All operations aborted.`
	got := parseConflictTargets(out)
	want := []string{".config/foo", ".config/bar"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("target %d: got %q want %q", i, got[i], want[i])
		}
	}
}

// TestParseConflictTargets_DifferentPackage covers the "stowed to a different
// package" phrasing, which appends " => <existing link dest>" that must be
// stripped from the target.
func TestParseConflictTargets_DifferentPackage(t *testing.T) {
	out := `  * existing target is stowed to a different package: .config/x => ../other/x`
	got := parseConflictTargets(out)
	if len(got) != 1 || got[0] != ".config/x" {
		t.Fatalf("got %v, want [.config/x]", got)
	}
}

func TestParseConflictTargets_Dedup(t *testing.T) {
	out := `  * cannot stow a/x over existing target .config/x since neither a link nor a directory and --adopt not specified
  * cannot stow b/x over existing target .config/x since neither a link nor a directory and --adopt not specified`
	got := parseConflictTargets(out)
	if len(got) != 1 || got[0] != ".config/x" {
		t.Fatalf("expected single deduped target, got %v", got)
	}
}

func TestParseConflictTargets_None(t *testing.T) {
	if got := parseConflictTargets("LINK: .config/foo => ../repo/foo\n"); len(got) != 0 {
		t.Fatalf("expected no targets from non-conflict output, got %v", got)
	}
}

func TestCommonParent(t *testing.T) {
	cases := []struct {
		paths []string
		want  string
	}{
		{[]string{".config/tmux/a", ".config/tmux/b/c"}, ".config/tmux"},
		{[]string{".config/tmux/aa", ".config/tmux/ab"}, ".config/tmux"}, // component-wise, not byte-prefix
		{[]string{".config/tmux", ".config/nvim"}, ".config"},
		{[]string{".zshrc", ".config/foo"}, ""},
		{[]string{".config/x"}, ".config/x"},
		{nil, ""},
	}
	for _, c := range cases {
		if got := commonParent(c.paths); got != c.want {
			t.Errorf("commonParent(%v) = %q, want %q", c.paths, got, c.want)
		}
	}
}

func TestConflictErrorMessage(t *testing.T) {
	cases := []struct {
		name    string
		targets []string
		substr  string
	}{
		{"single", []string{".config/x"}, "~/.config/x already exists"},
		{"common-parent", []string{".config/tmux/a", ".config/tmux/b"}, "~/.config/tmux already has 2 files"},
		{"no-parent", []string{".zshrc", ".config/foo"}, "2 existing files conflict"},
	}
	for _, c := range cases {
		e := &ConflictError{Module: "m", Targets: c.targets}
		if msg := e.Error(); !strings.Contains(msg, c.substr) {
			t.Errorf("%s: Error()=%q, want substring %q", c.name, msg, c.substr)
		}
	}
}

func TestFirstLine(t *testing.T) {
	if got := firstLine("\n\n  hello \nworld"); got != "hello" {
		t.Errorf("firstLine = %q, want %q", got, "hello")
	}
	if got := firstLine("   \n\t\n"); got != "" {
		t.Errorf("firstLine of blank = %q, want empty", got)
	}
}
