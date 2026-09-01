package main

import "testing"

func TestChangedModules(t *testing.T) {
	got := changedModules([]string{
		"atuin/.config/atuin/config.toml",
		"zsh/.zshrc",
		"zsh/.zprofile",
		"README.md",
	})

	for _, name := range []string{"atuin", "zsh"} {
		if !got[name] {
			t.Errorf("expected %q to be dirty: %#v", name, got)
		}
	}
	if got["tmux"] {
		t.Errorf("did not expect tmux to be dirty: %#v", got)
	}
}
