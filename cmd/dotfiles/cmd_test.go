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

func TestPullTargets(t *testing.T) {
	mods := []config.Module{
		{Name: "a", IsStowed: true},
		{Name: "b", IsStowed: false},
		{Name: "c", IsStowed: true},
	}
	// No args => only stowed modules.
	got, err := pullTargets(mods, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "c" {
		t.Errorf("pullTargets(nil) = %v", names(got))
	}
	// Explicit args => exactly those.
	got, _ = pullTargets(mods, []string{"b"})
	if len(got) != 1 || got[0].Name != "b" {
		t.Errorf("pullTargets([b]) = %v", names(got))
	}
}

func names(mods []config.Module) []string {
	var out []string
	for _, m := range mods {
		out = append(out, m.Name)
	}
	return out
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
