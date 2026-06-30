package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadManifest_Absent(t *testing.T) {
	m, err := LoadManifest(t.TempDir())
	if err != nil {
		t.Fatalf("absent manifest should not error: %v", err)
	}
	if len(m.Bootstrap.CoreBrew) != 0 || len(m.Bootstrap.GlobalBrew) != 0 {
		t.Errorf("absent manifest should be zero, got %+v", m)
	}
}

func TestLoadManifest_Present(t *testing.T) {
	dir := t.TempDir()
	content := `[bootstrap]
core_brew = ["git", "stow"]
global_brew = ["fzf", "jq"]
backup_targets = [".zshrc"]
`
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Bootstrap.CoreBrew) != 2 || m.Bootstrap.CoreBrew[1] != "stow" {
		t.Errorf("core_brew = %v", m.Bootstrap.CoreBrew)
	}
	if len(m.Bootstrap.GlobalBrew) != 2 || m.Bootstrap.GlobalBrew[0] != "fzf" {
		t.Errorf("global_brew = %v", m.Bootstrap.GlobalBrew)
	}
	if len(m.Bootstrap.BackupTargets) != 1 || m.Bootstrap.BackupTargets[0] != ".zshrc" {
		t.Errorf("backup_targets = %v", m.Bootstrap.BackupTargets)
	}
}

func TestLoadManifest_Invalid(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), []byte("not = = valid toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(dir); err == nil {
		t.Error("invalid manifest should error")
	}
}
