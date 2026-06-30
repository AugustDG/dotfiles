package platform

import (
	"path/filepath"
	"testing"
)

func TestExpandHome(t *testing.T) {
	home := HomeDir()
	cases := map[string]string{
		"~":        home,
		"~/x":      filepath.Join(home, "x"),
		"~/a/b":    filepath.Join(home, "a", "b"),
		"/abs":     "/abs",
		"rel/path": "rel/path",
		"~user":    "~user", // only a bare ~ or ~/ is expanded
	}
	for in, want := range cases {
		if got := ExpandHome(in); got != want {
			t.Errorf("ExpandHome(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPathContains(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/opt/homebrew/bin:"+filepath.Join("/home", "u", ".local", "bin"))
	if !PathContains("/opt/homebrew/bin") {
		t.Error("expected /opt/homebrew/bin to be on PATH")
	}
	if !PathContains("/opt/homebrew/bin/") { // trailing slash normalised
		t.Error("expected trailing-slash variant to match")
	}
	if PathContains("/not/on/path") {
		t.Error("did not expect /not/on/path to match")
	}
}

func TestEditor_EnvPrecedence(t *testing.T) {
	t.Setenv("VISUAL", "myvisual")
	t.Setenv("EDITOR", "myeditor")
	if got := Editor(); got != "myvisual" {
		t.Errorf("VISUAL should win, got %q", got)
	}

	t.Setenv("VISUAL", "")
	if got := Editor(); got != "myeditor" {
		t.Errorf("EDITOR should be used when VISUAL empty, got %q", got)
	}
}
