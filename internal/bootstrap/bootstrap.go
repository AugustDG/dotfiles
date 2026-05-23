package bootstrap

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/AugustDG/dotfiles/internal/brew"
	"github.com/AugustDG/dotfiles/internal/config"
	gitops "github.com/AugustDG/dotfiles/internal/git"
	"github.com/AugustDG/dotfiles/internal/platform"
	"github.com/AugustDG/dotfiles/internal/stow"
	"github.com/AugustDG/dotfiles/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

type Installer struct {
	program     *tea.Program
	dotfilesDir string
	homeDir     string
}

func NewInstaller(p *tea.Program, dotfilesDir string) *Installer {
	return &Installer{
		program:     p,
		dotfilesDir: dotfilesDir,
		homeDir:     platform.HomeDir(),
	}
}

func (inst *Installer) send(msg tea.Msg) {
	if inst.program != nil {
		inst.program.Send(msg)
	}
}

func (inst *Installer) bootstrapStep(name string, fn func() error) error {
	inst.send(tui.BootstrapStepMsg{Step: name})
	err := fn()
	inst.send(tui.BootstrapStepMsg{Step: name, Done: true, Err: err})
	return err
}

func (inst *Installer) RunBootstrap() error {
	if err := inst.bootstrapStep("Install Homebrew", func() error {
		if brew.IsInstalled() {
			return nil
		}
		if err := brew.Install(); err != nil {
			return err
		}
		brew.EnsureOnPath()
		return nil
	}); err != nil {
		return fmt.Errorf("homebrew install failed: %w", err)
	}

	brew.EnsureOnPath()

	_ = inst.bootstrapStep("Install core packages", func() error {
		return brew.InstallPackages(CoreBrewPackages)
	})

	_ = inst.bootstrapStep("GitHub auth", func() error {
		if gitops.IsGHAuthenticated() {
			return nil
		}
		return gitops.GHAuthLogin()
	})

	_ = inst.bootstrapStep("Clone dotfiles repo", func() error {
		if _, err := os.Stat(filepath.Join(inst.dotfilesDir, ".git")); err == nil {
			return nil
		}
		return gitops.CloneRepo(gitops.RepoSSH, inst.dotfilesDir)
	})

	_ = inst.bootstrapStep("Install global packages", func() error {
		return brew.InstallPackages(GlobalBrewPackages)
	})

	_ = inst.bootstrapStep("Install znap", func() error {
		znapPath := filepath.Join(inst.homeDir, ZnapDir)
		if _, err := os.Stat(znapPath); err == nil {
			return nil
		}
		os.MkdirAll(filepath.Dir(znapPath), 0o755)
		cmd := exec.Command("git", "clone", "--depth", "1", ZnapURL, znapPath)
		return cmd.Run()
	})

	_ = inst.bootstrapStep("Install hopper", func() error {
		if _, err := exec.LookPath("hopper"); err == nil {
			return nil
		}
		return installHopper()
	})

	_ = inst.bootstrapStep("Backup conflicting files", func() error {
		return backupConflicts(inst.homeDir)
	})

	_ = inst.bootstrapStep("Create ~/.zshrc.local", func() error {
		localrc := filepath.Join(inst.homeDir, ".zshrc.local")
		if _, err := os.Stat(localrc); err == nil {
			return nil
		}
		return os.WriteFile(localrc, []byte(ZshrcLocalTemplate), 0o600)
	})

	return nil
}

func (inst *Installer) InstallModule(mod config.Module) tui.ModuleResult {
	currentOS := platform.DetectOS()
	if !mod.SupportsOS(currentOS) {
		return tui.ModuleResult{Name: mod.Name, Status: "skipped", Warning: "wrong OS"}
	}

	if mod.HasSubmodule {
		inst.send(tui.StepStartMsg{Module: mod.Name, Step: "Init submodules"})
		subPaths := submodulePathsForModule(inst.dotfilesDir, mod.Name)
		err := gitops.InitSubmodules(inst.dotfilesDir, subPaths)
		inst.send(tui.StepDoneMsg{Module: mod.Name, Step: "Init submodules", Err: err})
		if err != nil {
			return tui.ModuleResult{Name: mod.Name, Status: "failed", Warning: err.Error()}
		}
	}

	if len(mod.Deps.Brew) > 0 {
		inst.send(tui.StepStartMsg{Module: mod.Name, Step: fmt.Sprintf("Install %s", strings.Join(mod.Deps.Brew, ", "))})
		err := brew.InstallPackages(mod.Deps.Brew)
		inst.send(tui.StepDoneMsg{Module: mod.Name, Step: fmt.Sprintf("Install %s", strings.Join(mod.Deps.Brew, ", ")), Err: err})
		if err != nil {
			return tui.ModuleResult{Name: mod.Name, Status: "failed", Warning: err.Error()}
		}
	}

	inst.send(tui.StepStartMsg{Module: mod.Name, Step: "Stow"})
	err := stow.Stow(inst.dotfilesDir, mod.Name, inst.homeDir)
	inst.send(tui.StepDoneMsg{Module: mod.Name, Step: "Stow", Err: err})
	if err != nil {
		return tui.ModuleResult{Name: mod.Name, Status: "failed", Warning: err.Error()}
	}

	if mod.Hooks.PostInstall != "" {
		hookCmd := platform.ExpandHome(mod.Hooks.PostInstall)
		inst.send(tui.StepStartMsg{Module: mod.Name, Step: "Post-install hook"})
		cmd := exec.Command("bash", "-c", hookCmd)
		cmd.Stdout = nil
		cmd.Stderr = nil
		err := cmd.Run()
		inst.send(tui.StepDoneMsg{Module: mod.Name, Step: "Post-install hook", Err: err})
		if err != nil {
			return tui.ModuleResult{Name: mod.Name, Status: "installed", Warning: "hook failed: " + err.Error()}
		}
	}

	return tui.ModuleResult{Name: mod.Name, Status: "installed"}
}

func submodulePathsForModule(dotfilesDir, moduleName string) []string {
	gitmodules := filepath.Join(dotfilesDir, ".gitmodules")
	data, err := os.ReadFile(gitmodules)
	if err != nil {
		return []string{moduleName}
	}
	var paths []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "path = ") {
			p := strings.TrimPrefix(line, "path = ")
			if strings.HasPrefix(p, moduleName+"/") || strings.HasPrefix(p, moduleName+"/.") {
				paths = append(paths, p)
			}
		}
	}
	if len(paths) == 0 {
		return []string{moduleName}
	}
	return paths
}

func installHopper() error {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	asset := fmt.Sprintf("hopper_%s_%s.tar.gz", goos, goarch)
	url := fmt.Sprintf("https://github.com/%s/releases/latest/download/%s", HopperRepo, asset)

	tmpDir, err := os.MkdirTemp("", "hopper-install-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	tarball := filepath.Join(tmpDir, asset)
	dl := exec.Command("curl", "-fsSL", "-o", tarball, url)
	if err := dl.Run(); err != nil {
		return fmt.Errorf("download hopper: %w", err)
	}

	extract := exec.Command("tar", "-xzf", tarball, "-C", tmpDir)
	if err := extract.Run(); err != nil {
		return fmt.Errorf("extract hopper: %w", err)
	}

	binDir := filepath.Join(platform.HomeDir(), ".local", "bin")
	os.MkdirAll(binDir, 0o755)

	src := filepath.Join(tmpDir, "hopper")
	dst := filepath.Join(binDir, "hopper")
	cp := exec.Command("cp", src, dst)
	if err := cp.Run(); err != nil {
		return fmt.Errorf("install hopper binary: %w", err)
	}
	os.Chmod(dst, 0o755)
	return nil
}

func backupConflicts(homeDir string) error {
	needBackup := false
	for _, f := range BackupTargets {
		p := filepath.Join(homeDir, f)
		info, err := os.Lstat(p)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink == 0 {
			needBackup = true
			break
		}
	}
	if !needBackup {
		return nil
	}

	backupDir := filepath.Join(homeDir, fmt.Sprintf(".dotfiles-backup-%s", time.Now().Format("20060102-150405")))
	os.MkdirAll(backupDir, 0o755)

	for _, f := range BackupTargets {
		src := filepath.Join(homeDir, f)
		info, err := os.Lstat(src)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		dst := filepath.Join(backupDir, f)
		os.Rename(src, dst)
	}
	return nil
}
