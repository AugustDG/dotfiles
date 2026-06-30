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
	"github.com/AugustDG/dotfiles/internal/runner"
	"github.com/AugustDG/dotfiles/internal/stow"
	"github.com/AugustDG/dotfiles/internal/syspkg"
	"github.com/AugustDG/dotfiles/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

type Installer struct {
	program     *tea.Program
	dotfilesDir string
	homeDir     string
	Verbose     bool
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
	if inst.program == nil {
		fmt.Printf("  ... %s", name)
	} else {
		inst.send(tui.BootstrapStepMsg{Step: name})
	}
	err := fn()
	if inst.program == nil {
		if err != nil {
			fmt.Printf(" — failed: %s\n", err)
		} else {
			fmt.Print(" ✓\n")
		}
	} else {
		inst.send(tui.BootstrapStepMsg{Step: name, Done: true, Err: err})
	}
	return err
}

func (inst *Installer) RunBootstrap() error {
	if runtime.GOOS == "linux" {
		_ = inst.bootstrapStep("Install Linux prerequisites", func() error {
			return syspkg.InstallPrereqs()
		})
	}

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
		return brew.InstallPackages(orDefault(inst.manifest().CoreBrew, CoreBrewPackages))
	})

	_ = inst.bootstrapStep("GitHub auth", func() error {
		if gitops.IsGHAuthenticated() {
			return nil
		}
		if !platform.HasTTY() {
			return nil
		}
		return gitops.GHAuthLogin()
	})

	_ = inst.bootstrapStep("Set default shell to zsh", func() error {
		return setDefaultShell()
	})

	_ = inst.bootstrapStep("Clone dotfiles repo", func() error {
		if _, err := os.Stat(filepath.Join(inst.dotfilesDir, ".git")); err == nil {
			return nil
		}
		return gitops.CloneRepo(gitops.RepoSSH, inst.dotfilesDir)
	})

	_ = inst.bootstrapStep("Install global packages", func() error {
		return brew.InstallPackages(orDefault(inst.manifest().GlobalBrew, GlobalBrewPackages))
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
		return backupConflicts(inst.homeDir, orDefault(inst.manifest().BackupTargets, BackupTargets))
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
		err := gitops.InitSubmodules(inst.dotfilesDir, mod.SubmodulePaths)
		inst.send(tui.StepDoneMsg{Module: mod.Name, Step: "Init submodules", Err: err})
		if err != nil {
			return tui.ModuleResult{Name: mod.Name, Status: "failed", Warning: err.Error()}
		}
	}

	if !mod.Deps.Empty() {
		label := fmt.Sprintf("Install %s", strings.Join(DepNames(mod.Deps), ", "))
		inst.send(tui.StepStartMsg{Module: mod.Name, Step: label})
		err := InstallDeps(mod.Deps)
		inst.send(tui.StepDoneMsg{Module: mod.Name, Step: label, Err: err})
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
		runner.ConfigureCmd(cmd)
		err := cmd.Run()
		inst.send(tui.StepDoneMsg{Module: mod.Name, Step: "Post-install hook", Err: err})
		if err != nil {
			return tui.ModuleResult{Name: mod.Name, Status: "installed", Warning: "hook failed: " + err.Error()}
		}
	}

	return tui.ModuleResult{Name: mod.Name, Status: "installed"}
}

// manifest loads the bootstrap section of dotfiles.toml, returning a zero value
// when the manifest is absent so callers fall back to built-in defaults.
func (inst *Installer) manifest() config.BootstrapConfig {
	m, _ := config.LoadManifest(inst.dotfilesDir)
	return m.Bootstrap
}

// orDefault returns v when it is non-empty, otherwise def.
func orDefault(v, def []string) []string {
	if len(v) > 0 {
		return v
	}
	return def
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

func backupConflicts(homeDir string, targets []string) error {
	needBackup := false
	for _, f := range targets {
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
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return fmt.Errorf("create backup dir: %w", err)
	}

	for _, f := range targets {
		src := filepath.Join(homeDir, f)
		info, err := os.Lstat(src)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		dst := filepath.Join(backupDir, f)
		// Create parent dirs so nested backup_targets (e.g. ".config/x") work.
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("backup %s: %w", f, err)
		}
		if err := os.Rename(src, dst); err != nil {
			// Cross-device move or other rename failure: copy then remove.
			if cpErr := exec.Command("cp", "-a", src, dst).Run(); cpErr != nil {
				return fmt.Errorf("backup %s: %w", f, err)
			}
			if rmErr := os.RemoveAll(src); rmErr != nil {
				return fmt.Errorf("backup %s: remove original: %w", f, rmErr)
			}
		}
	}
	return nil
}

func setDefaultShell() error {
	zshBin := findZsh()
	if zshBin == "" {
		return fmt.Errorf("no zsh found")
	}

	// Always operate on the human user (under sudo, $USER is "root"; SUDO_USER
	// holds the original account). Compare against the configured login shell —
	// not $SHELL, which reflects the currently-running shell, not the default.
	user := targetUser()
	if currentLoginShell(user) == zshBin {
		return nil
	}

	shells, _ := os.ReadFile("/etc/shells")
	if !strings.Contains(string(shells), zshBin) {
		cmd := runner.Sudo("tee", "-a", "/etc/shells")
		cmd.Stdin = strings.NewReader(zshBin + "\n")
		cmd.Stdout = nil
		cmd.Run()
	}

	// runner.Sudo prepends sudo only when not already root; passing the target
	// user explicitly makes both the root and non-root paths change the right
	// account.
	return runner.Sudo("chsh", "-s", zshBin, user).Run()
}

func findZsh() string {
	for _, candidate := range []string{
		"/opt/homebrew/bin/zsh",
		"/home/linuxbrew/.linuxbrew/bin/zsh",
		"/usr/local/bin/zsh",
		"/bin/zsh",
		"/usr/bin/zsh",
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// targetUser returns the human account whose shell should be changed, resolving
// the original user when running under sudo.
func targetUser() string {
	if u := os.Getenv("SUDO_USER"); u != "" && u != "root" {
		return u
	}
	return os.Getenv("USER")
}

// currentLoginShell returns user's configured login shell from the user
// database (Directory Service on macOS, passwd on Linux), or "" if unknown.
func currentLoginShell(user string) string {
	if user == "" {
		return ""
	}
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("dscl", ".", "-read", "/Users/"+user, "UserShell").Output()
		if err != nil {
			return ""
		}
		fields := strings.Fields(string(out)) // "UserShell: /bin/zsh"
		if len(fields) >= 2 {
			return fields[len(fields)-1]
		}
		return ""
	}
	out, err := exec.Command("getent", "passwd", user).Output()
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.TrimSpace(string(out)), ":")
	if len(parts) >= 7 {
		return parts[6]
	}
	return ""
}
