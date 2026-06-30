package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/AugustDG/dotfiles/internal/config"
)

func TestValidateModuleName(t *testing.T) {
	valid := []string{"nvim", "zsh", "my-module", "mod_1"}
	for _, n := range valid {
		if err := validateModuleName(n); err != nil {
			t.Errorf("validateModuleName(%q) unexpected error: %v", n, err)
		}
	}
	invalid := []string{"", ".", "..", ".hidden", "a/b", "a\\b"}
	for _, n := range invalid {
		if err := validateModuleName(n); err == nil {
			t.Errorf("validateModuleName(%q) expected error", n)
		}
	}
}

func TestFirstError(t *testing.T) {
	if err := firstError([]error{nil, nil}); err != nil {
		t.Errorf("all-nil should be nil, got %v", err)
	}
	e := errors.New("boom")
	if err := firstError([]error{nil, e, nil}); !errors.Is(err, e) {
		t.Errorf("single error should be returned, got %v", err)
	}
	multi := firstError([]error{errors.New("a"), errors.New("b")})
	if multi == nil {
		t.Error("multiple errors should produce a non-nil error")
	}
}

func TestWithin(t *testing.T) {
	dir := "/home/u/repo"
	if !within("/home/u/repo/x", dir) {
		t.Error("nested path should be within")
	}
	if within("/home/u/other", dir) {
		t.Error("sibling path should not be within")
	}
}

func TestScaffoldModule(t *testing.T) {
	dir := t.TempDir()
	moduleDir, created, err := scaffoldModule(dir, "newmod", "a desc", []string{"darwin"})
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected created=true on first scaffold")
	}
	data, err := os.ReadFile(filepath.Join(moduleDir, "module.toml"))
	if err != nil {
		t.Fatal(err)
	}
	mod, err := config.LoadModule(moduleDir)
	if err != nil {
		t.Fatalf("scaffolded module.toml does not parse: %v\n%s", err, data)
	}
	if mod.Name != "newmod" || mod.Description != "a desc" {
		t.Errorf("parsed module = %+v", mod)
	}
	if len(mod.OS) != 1 || mod.OS[0] != "darwin" {
		t.Errorf("os = %v", mod.OS)
	}

	// Second call must report not-created.
	if _, created, _ := scaffoldModule(dir, "newmod", "", nil); created {
		t.Error("expected created=false when module already exists")
	}
}

func TestResolveModuleArgs(t *testing.T) {
	mods := []config.Module{{Name: "a"}, {Name: "b"}}
	got, err := resolveModuleArgs(mods, []string{"b"})
	if err != nil || len(got) != 1 || got[0].Name != "b" {
		t.Fatalf("resolveModuleArgs returned %v, %v", got, err)
	}
	if _, err := resolveModuleArgs(mods, []string{"missing"}); err == nil {
		t.Error("unknown module should error")
	}
}

func names(mods []config.Module) []string {
	var out []string
	for _, m := range mods {
		out = append(out, m.Name)
	}
	return out
}

// TestRunAdoptRollback verifies that when a later path fails to adopt, an
// earlier already-moved file is restored to its original $HOME location rather
// than left moved-but-unstowed. It does not require the stow binary because the
// failure occurs before the stow step.
func TestRunAdoptRollback(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	df := filepath.Join(root, "df")
	mustMkdir(t, home)
	mustMkdir(t, filepath.Join(df, "m"))
	t.Setenv("HOME", home)
	t.Setenv("DOTFILES_DIR", df)

	aPath := filepath.Join(home, ".a")
	bPath := filepath.Join(home, ".b")
	mustWriteFile(t, aPath, "aaa")
	mustWriteFile(t, bPath, "bbb")
	// Conflicting .b already in the module makes the second adopt fail.
	mustWriteFile(t, filepath.Join(df, "m", "module.toml"), "name='m'")
	mustWriteFile(t, filepath.Join(df, "m", ".b"), "existing")

	if err := runAdopt("m", []string{aPath, bPath}); err == nil {
		t.Fatal("expected error when the second path conflicts")
	}

	fi, err := os.Lstat(aPath)
	if err != nil {
		t.Fatalf(".a was not restored to $HOME: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Error(".a should be a regular file after rollback, not a symlink")
	}
	if data, _ := os.ReadFile(aPath); string(data) != "aaa" {
		t.Error(".a content lost after rollback")
	}
	if _, err := os.Lstat(filepath.Join(df, "m", ".a")); err == nil {
		t.Error("module should not retain .a after rollback")
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCompatibleModules(t *testing.T) {
	mods := []config.Module{
		{Name: "x", OS: []string{"darwin"}},
		{Name: "y", OS: []string{"linux"}},
		{Name: "z"}, // no restriction
	}
	got := compatibleModules(mods, "darwin")
	if len(got) != 2 {
		t.Fatalf("expected x and z, got %v", names(got))
	}
}
