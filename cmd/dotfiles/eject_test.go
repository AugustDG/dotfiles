package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AugustDG/dotfiles/internal/config"
)

func TestMaterializeModule(t *testing.T) {
	root := t.TempDir()
	moduleDir := filepath.Join(root, "module")
	homeDir := filepath.Join(root, "home")
	mustMkdir(t, filepath.Join(moduleDir, ".config", "app"))
	mustMkdir(t, filepath.Join(homeDir, ".config"))
	mustWriteFile(t, filepath.Join(moduleDir, "module.toml"), "name='app'")
	mustWriteFile(t, filepath.Join(moduleDir, ".config", "app", "config.toml"), "setting=true")
	if err := os.Symlink("config.toml", filepath.Join(moduleDir, ".config", "app", "current")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(
		filepath.Join(moduleDir, ".config", "app", "config.toml"),
		filepath.Join(moduleDir, ".config", "app", "absolute"),
	); err != nil {
		t.Fatal(err)
	}

	res, err := materializeModule(moduleDir, homeDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.created) == 0 {
		t.Fatal("expected materialized paths to be reported")
	}
	configPath := filepath.Join(homeDir, ".config", "app", "config.toml")
	if data, err := os.ReadFile(configPath); err != nil || string(data) != "setting=true" {
		t.Fatalf("materialized config = %q, %v", data, err)
	}
	if target, err := os.Readlink(filepath.Join(homeDir, ".config", "app", "current")); err != nil || target != "config.toml" {
		t.Fatalf("materialized symlink = %q, %v", target, err)
	}
	wantAbsolute := filepath.Join(homeDir, ".config", "app", "config.toml")
	if target, err := os.Readlink(filepath.Join(homeDir, ".config", "app", "absolute")); err != nil || target != wantAbsolute {
		t.Fatalf("rewritten absolute symlink = %q, %v; want %q", target, err, wantAbsolute)
	}
	if _, err := os.Stat(filepath.Join(homeDir, "module.toml")); !os.IsNotExist(err) {
		t.Error("module.toml must not be copied into $HOME")
	}
}

func TestPreflightMaterialize(t *testing.T) {
	root := t.TempDir()
	moduleDir := filepath.Join(root, "module")
	homeDir := filepath.Join(root, "home")
	source := filepath.Join(moduleDir, ".config", "app", "config.toml")
	target := filepath.Join(homeDir, ".config", "app", "config.toml")
	mustMkdir(t, filepath.Dir(source))
	mustMkdir(t, filepath.Dir(target))
	mustWriteFile(t, filepath.Join(moduleDir, "module.toml"), "name='app'")
	mustWriteFile(t, source, "managed")
	if err := os.Symlink(source, target); err != nil {
		t.Fatal(err)
	}

	if err := preflightMaterialize(moduleDir, homeDir); err != nil {
		t.Fatalf("managed Stow link should pass preflight: %v", err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, target, "unmanaged")
	if err := preflightMaterialize(moduleDir, homeDir); err == nil {
		t.Fatal("unmanaged target should fail preflight")
	}
}

func TestPreflightMaterializeAllowsFoldedStowDirectory(t *testing.T) {
	root := t.TempDir()
	moduleDir := filepath.Join(root, "module")
	homeDir := filepath.Join(root, "home")
	sourceDir := filepath.Join(moduleDir, ".config", "app")
	targetDir := filepath.Join(homeDir, ".config", "app")
	mustMkdir(t, sourceDir)
	mustMkdir(t, filepath.Dir(targetDir))
	mustWriteFile(t, filepath.Join(moduleDir, "module.toml"), "name='app'")
	mustWriteFile(t, filepath.Join(sourceDir, "config.toml"), "managed")
	if err := os.Symlink(sourceDir, targetDir); err != nil {
		t.Fatal(err)
	}

	if err := preflightMaterialize(moduleDir, homeDir); err != nil {
		t.Fatalf("folded Stow directory should pass preflight: %v", err)
	}
}

// TestMaterializeModuleRewritesRelativeLinks covers links whose relative
// target would point somewhere else once the link moves from the module into
// $HOME, like agents/.codex/AGENTS.md -> ../../claude/.claude/CLAUDE.md.
func TestMaterializeModuleRewritesRelativeLinks(t *testing.T) {
	root := t.TempDir()
	dotfilesDir := filepath.Join(root, "dotfiles")
	moduleDir := filepath.Join(dotfilesDir, "agents")
	homeDir := filepath.Join(root, "home")
	mustMkdir(t, filepath.Join(moduleDir, ".codex"))
	mustMkdir(t, filepath.Join(dotfilesDir, "other", ".claude"))
	mustMkdir(t, homeDir)
	mustWriteFile(t, filepath.Join(dotfilesDir, "other", ".claude", "CLAUDE.md"), "instructions")
	mustWriteFile(t, filepath.Join(moduleDir, ".apprc"), "rc")

	links := map[string]string{
		"outside":  "../../other/.claude/CLAUDE.md",  // another module, stays in the repo
		"inside":   "../.apprc",                      // this module, moves into $HOME
		"dangling": "../../claude/.claude/CLAUDE.md", // gone
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(moduleDir, ".codex", name)); err != nil {
			t.Fatal(err)
		}
	}

	res, err := materializeModule(moduleDir, homeDir)
	if err != nil {
		t.Fatal(err)
	}

	for name, want := range map[string]string{"outside": "instructions", "inside": "rc"} {
		link := filepath.Join(homeDir, ".codex", name)
		target, err := os.Readlink(link)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if filepath.IsAbs(target) {
			t.Errorf("%s: relative link became absolute: %s", name, target)
		}
		if data, err := os.ReadFile(link); err != nil || string(data) != want {
			t.Errorf("%s -> %s reads %q, %v; want %q", name, target, data, err, want)
		}
	}

	if _, err := os.Lstat(filepath.Join(homeDir, ".codex", "dangling")); !os.IsNotExist(err) {
		t.Error("a link with a missing target must not be recreated")
	}
	if len(res.skipped) != 1 || !strings.Contains(res.skipped[0], "dangling") {
		t.Errorf("skipped = %q, want the dangling link reported", res.skipped)
	}
}

func TestMaterializeModuleRefusesToOverwrite(t *testing.T) {
	root := t.TempDir()
	moduleDir := filepath.Join(root, "module")
	homeDir := filepath.Join(root, "home")
	mustMkdir(t, filepath.Join(moduleDir, ".config", "app"))
	mustMkdir(t, filepath.Join(homeDir, ".config", "app"))
	mustWriteFile(t, filepath.Join(moduleDir, ".config", "app", "config.toml"), "managed")
	target := filepath.Join(homeDir, ".config", "app", "config.toml")
	mustWriteFile(t, target, "local")

	res, err := materializeModule(moduleDir, homeDir)
	if err == nil {
		t.Fatal("expected an overwrite conflict")
	}
	removeCreatedPaths(res.created)
	if data, readErr := os.ReadFile(target); readErr != nil || string(data) != "local" {
		t.Fatalf("existing target was changed: %q, %v", data, readErr)
	}
}

func TestRunEjectRejectsSubmoduleBeforeMutation(t *testing.T) {
	root := t.TempDir()
	dotfilesDir := filepath.Join(root, "dotfiles")
	homeDir := filepath.Join(root, "home")
	moduleDir := filepath.Join(dotfilesDir, "app")
	mustMkdir(t, filepath.Join(moduleDir, "sub"))
	mustMkdir(t, homeDir)
	mustWriteFile(t, filepath.Join(moduleDir, "module.toml"), "name='app'")
	mustWriteFile(t, filepath.Join(dotfilesDir, ".gitmodules"), "[submodule \"app/sub\"]\n\tpath = app/sub\n\turl = example.invalid/repo\n")
	t.Setenv("HOME", homeDir)
	t.Setenv("DOTFILES_DIR", dotfilesDir)

	err := runEject([]string{"app"}, true, false)
	if err == nil || !strings.Contains(err.Error(), "contains Git submodules") {
		t.Fatalf("expected submodule safety error, got %v", err)
	}
	if _, statErr := os.Stat(moduleDir); statErr != nil {
		t.Fatalf("module was mutated despite preflight failure: %v", statErr)
	}
}

func TestRunEjectWithSubmodules(t *testing.T) {
	root := t.TempDir()
	subRepo := filepath.Join(root, "subrepo")
	dotfilesDir := filepath.Join(root, "dotfiles")
	homeDir := filepath.Join(root, "home")
	mustMkdir(t, subRepo)
	mustMkdir(t, homeDir)
	runGitTest(t, subRepo, "init", "-q")
	runGitTest(t, subRepo, "config", "user.email", "test@example.com")
	runGitTest(t, subRepo, "config", "user.name", "Test")
	mustWriteFile(t, filepath.Join(subRepo, "sub.conf"), "submodule")
	runGitTest(t, subRepo, "add", ".")
	runGitTest(t, subRepo, "commit", "-qm", "init")

	mustMkdir(t, filepath.Join(dotfilesDir, "app"))
	runGitTest(t, dotfilesDir, "init", "-q")
	runGitTest(t, dotfilesDir, "config", "user.email", "test@example.com")
	runGitTest(t, dotfilesDir, "config", "user.name", "Test")
	mustWriteFile(t, filepath.Join(dotfilesDir, "app", "module.toml"), "name='app'")
	mustWriteFile(t, filepath.Join(dotfilesDir, "app", "app.conf"), "app")
	cmd := exec.Command("git", "-C", dotfilesDir, "-c", "protocol.file.allow=always", "submodule", "add", "-q", subRepo, "app/sub")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("add submodule: %v: %s", err, out)
	}
	runGitTest(t, dotfilesDir, "add", ".")
	runGitTest(t, dotfilesDir, "commit", "-qm", "init")

	binDir := filepath.Join(root, "bin")
	mustMkdir(t, binDir)
	mustWriteFile(t, filepath.Join(binDir, "stow"), "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(filepath.Join(binDir, "stow"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", homeDir)
	t.Setenv("DOTFILES_DIR", dotfilesDir)

	// Dirty nested worktrees are rejected before anything is unstowed.
	mustWriteFile(t, filepath.Join(dotfilesDir, "app", "sub", "sub.conf"), "dirty")
	if err := runEject([]string{"app"}, true, true); err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("expected dirty-submodule error, got %v", err)
	}
	runGitTest(t, filepath.Join(dotfilesDir, "app", "sub"), "checkout", "--", "sub.conf")

	if err := runEject([]string{"app"}, true, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dotfilesDir, "app")); !os.IsNotExist(err) {
		t.Fatalf("module directory still exists: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(homeDir, "sub", "sub.conf")); err != nil || string(data) != "submodule" {
		t.Fatalf("preserved submodule file = %q, %v", data, err)
	}
	if _, err := os.Lstat(filepath.Join(homeDir, "sub", ".git")); !os.IsNotExist(err) {
		t.Fatalf("submodule .git metadata was materialized: %v", err)
	}
	gitmodules, err := os.ReadFile(filepath.Join(dotfilesDir, ".gitmodules"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gitmodules), "app/sub") {
		t.Fatalf("submodule registration remains:\n%s", gitmodules)
	}
	if _, err := os.Stat(filepath.Join(dotfilesDir, ".git", "modules", "app", "sub", "HEAD")); err != nil {
		t.Fatalf("recoverable submodule Git cache was removed: %v", err)
	}
	if out, err := exec.Command("git", "-C", dotfilesDir, "ls-files", "--stage", "--", "app/sub").Output(); err != nil || len(out) != 0 {
		t.Fatalf("removed submodule gitlink remains: %v: %s", err, out)
	}
	// The intermediate working tree remains valid for normal sync operations.
	runGitTest(t, dotfilesDir, "submodule", "update", "--init", "--recursive")
}

func TestEjectModuleDeleteFiles(t *testing.T) {
	root := t.TempDir()
	dotfilesDir := filepath.Join(root, "dotfiles")
	homeDir := filepath.Join(root, "home")
	moduleDir := filepath.Join(dotfilesDir, "app")
	source := filepath.Join(moduleDir, ".apprc")
	target := filepath.Join(homeDir, ".apprc")
	mustMkdir(t, moduleDir)
	mustMkdir(t, homeDir)
	mustWriteFile(t, filepath.Join(moduleDir, "module.toml"), "name='app'")
	mustWriteFile(t, source, "managed")
	if err := os.Symlink(source, target); err != nil {
		t.Fatal(err)
	}

	installFakeUnstow(t, root, target)
	mod := config.Module{Name: "app", Path: moduleDir, IsStowed: true}
	if _, err := ejectModule(dotfilesDir, homeDir, mod, false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(moduleDir); !os.IsNotExist(err) {
		t.Fatalf("module directory still exists: %v", err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("managed target still exists: %v", err)
	}
}

func TestEjectModuleKeepFiles(t *testing.T) {
	root := t.TempDir()
	dotfilesDir := filepath.Join(root, "dotfiles")
	homeDir := filepath.Join(root, "home")
	moduleDir := filepath.Join(dotfilesDir, "app")
	source := filepath.Join(moduleDir, ".apprc")
	target := filepath.Join(homeDir, ".apprc")
	mustMkdir(t, moduleDir)
	mustMkdir(t, homeDir)
	mustWriteFile(t, filepath.Join(moduleDir, "module.toml"), "name='app'")
	mustWriteFile(t, source, "managed")
	if err := os.Symlink(source, target); err != nil {
		t.Fatal(err)
	}

	installFakeUnstow(t, root, target)
	mod := config.Module{Name: "app", Path: moduleDir, IsStowed: true}
	if _, err := ejectModule(dotfilesDir, homeDir, mod, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(moduleDir); !os.IsNotExist(err) {
		t.Fatalf("module directory still exists: %v", err)
	}
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("kept target missing: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("kept target should be a regular file")
	}
	if data, _ := os.ReadFile(target); string(data) != "managed" {
		t.Fatalf("kept target content = %q", data)
	}
}

func runGitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func installFakeUnstow(t *testing.T, root, target string) {
	t.Helper()
	// ejectModule invokes GNU Stow only to unstow on these successful paths.
	// This test double removes the target link just as `stow -D` would.
	binDir := filepath.Join(root, "bin")
	mustMkdir(t, binDir)
	stowScript := "#!/bin/sh\nrm -f \"$REMOVE_TARGET\"\n"
	mustWriteFile(t, filepath.Join(binDir, "stow"), stowScript)
	if err := os.Chmod(filepath.Join(binDir, "stow"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("REMOVE_TARGET", target)
}
