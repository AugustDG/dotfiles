package bootstrap

import (
	"context"
	"errors"
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
)

type Installer struct {
	dotfilesDir string
	homeDir     string
}

func NewInstaller(dotfilesDir string) *Installer {
	return &Installer{
		dotfilesDir: dotfilesDir,
		homeDir:     platform.HomeDir(),
	}
}

// BootstrapTasks returns the steps that set up the toolchain before modules
// are linked. A Homebrew failure stops the run; other steps are best-effort.
func (inst *Installer) BootstrapTasks() []tui.Task {
	var tasks []tui.Task
	if runtime.GOOS == "linux" {
		tasks = append(tasks, tui.Task{
			Title:       "Install Linux prerequisites",
			Run:         func(context.Context) error { return syspkg.InstallPrereqs() },
			Interactive: needsSudo(),
		})
	}

	tasks = append(tasks,
		tui.Task{
			Title: "Install Homebrew",
			Run: func(context.Context) error {
				if !brew.IsInstalled() {
					if err := brew.Install(); err != nil {
						return err
					}
				}
				brew.EnsureOnPath()
				return nil
			},
			Interactive: !brew.IsInstalled() && needsSudo(),
			Fatal:       true,
		},
		tui.Task{
			Title: "Install core packages",
			Run: func(context.Context) error {
				return brew.InstallPackages(orDefault(inst.manifest().CoreBrew, CoreBrewPackages))
			},
		},
		tui.Task{
			Title: "GitHub auth",
			Run: func(context.Context) error {
				if gitops.IsGHAuthenticated() || !platform.HasTTY() {
					return nil
				}
				return gitops.GHAuthLogin()
			},
			Interactive: platform.HasTTY() && !gitops.IsGHAuthenticated(),
		},
		tui.Task{
			Title:       "Set default shell to zsh",
			Run:         func(context.Context) error { return setDefaultShell() },
			Interactive: needsSudo() && !defaultShellIsZsh(),
		},
		tui.Task{
			Title: "Clone dotfiles repo",
			Run: func(context.Context) error {
				if _, err := os.Stat(filepath.Join(inst.dotfilesDir, ".git")); err == nil {
					return nil
				}
				return gitops.CloneRepo(gitops.RepoSSH, inst.dotfilesDir)
			},
		},
		tui.Task{
			Title: "Install global packages",
			Run: func(context.Context) error {
				return brew.InstallPackages(orDefault(inst.manifest().GlobalBrew, GlobalBrewPackages))
			},
		},
		tui.Task{
			Title: "Install znap",
			Run: func(context.Context) error {
				znapPath := filepath.Join(inst.homeDir, ZnapDir)
				if _, err := os.Stat(znapPath); err == nil {
					return nil
				}
				if err := os.MkdirAll(filepath.Dir(znapPath), 0o755); err != nil {
					return err
				}
				return exec.Command("git", "clone", "--depth", "1", ZnapURL, znapPath).Run()
			},
		},
		releaseBinaryTask(HopperRepo, "hopper"),
		releaseBinaryTask(GhottoRepo, "gho"),
		tui.Task{
			Title: "Back up conflicting files",
			Run: func(context.Context) error {
				return backupConflicts(inst.homeDir, orDefault(inst.manifest().BackupTargets, BackupTargets))
			},
		},
	)

	for _, local := range []struct{ name, template string }{
		{".zprofile.local", ZprofileLocalTemplate},
		{".zshrc.local", ZshrcLocalTemplate},
	} {
		tasks = append(tasks, tui.Task{
			Title: "Create ~/" + local.name,
			Run: func(context.Context) error {
				path := filepath.Join(inst.homeDir, local.name)
				if _, err := os.Stat(path); err == nil {
					return nil
				}
				return os.WriteFile(path, []byte(local.template), 0o600)
			},
		})
	}
	return tasks
}

func releaseBinaryTask(repo, bin string) tui.Task {
	return tui.Task{
		Title: "Install " + bin,
		Run: func(context.Context) error {
			if _, err := exec.LookPath(bin); err == nil {
				return nil
			}
			return installReleaseBinary(repo, bin)
		},
	}
}

// ModuleTasks returns the steps that link one module, grouped under its name:
// init submodules, install deps, stow, then run the post-install hook. A hook
// failure is reported as a warning since the module is already linked.
func (inst *Installer) ModuleTasks(mod config.Module, adopt bool) []tui.Task {
	group := mod.Name
	currentOS := platform.DetectOS()
	if !mod.SupportsOS(currentOS) {
		return []tui.Task{{
			Group: group,
			Title: "Link",
			Run:   func(context.Context) error { return tui.Skipped("not supported on " + currentOS) },
		}}
	}

	var tasks []tui.Task
	if mod.HasSubmodule {
		tasks = append(tasks, tui.Task{
			Group: group,
			Title: "Init submodules",
			Run: func(context.Context) error {
				return gitops.InitSubmodules(inst.dotfilesDir, mod.SubmodulePaths)
			},
		})
	}

	if !mod.Deps.Empty() {
		tasks = append(tasks, DepsTask(mod))
	}

	tasks = append(tasks, tui.Task{
		Group: group,
		Title: "Stow",
		Run: func(context.Context) error {
			if adopt {
				return stow.StowAdopt(inst.dotfilesDir, mod.Name, inst.homeDir)
			}
			err := stow.Stow(inst.dotfilesDir, mod.Name, inst.homeDir)
			var conflict *stow.ConflictError
			if errors.As(err, &conflict) {
				return tui.WithHint(err, "rerun with --adopt to absorb the existing files into the repo, or back them up first")
			}
			return err
		},
	})

	if mod.Hooks.PostInstall != "" {
		tasks = append(tasks, tui.Task{
			Group: group,
			Title: "Post-install hook",
			Run: func(ctx context.Context) error {
				cmd := exec.CommandContext(ctx, "bash", "-c", platform.ExpandHome(mod.Hooks.PostInstall))
				runner.ConfigureCmd(cmd)
				return tui.Warning(cmd.Run())
			},
		})
	}
	return tasks
}

// DepsTask returns the task that installs a module's dependencies.
func DepsTask(mod config.Module) tui.Task {
	title := "Install dependencies"
	if names := DepNames(mod.Deps); len(names) > 0 {
		title = "Install " + strings.Join(names, ", ")
	}
	return tui.Task{
		Group:       mod.Name,
		Title:       title,
		Run:         func(context.Context) error { return InstallDeps(mod.Deps) },
		Interactive: DepsNeedSudo(mod.Deps),
	}
}

func needsSudo() bool { return len(runner.SudoPrefix()) > 0 }

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

// installReleaseBinary downloads the latest GitHub release tarball for a
// GoReleaser-style repo and installs the contained binary into ~/.local/bin
// (which the dotfiles put on PATH). It assumes the asset is named
// "<bin>_<goos>_<goarch>.tar.gz" and unpacks to a single executable named
// <bin> — the convention shared by hopper (AugustDG/hopper) and gho
// (AugustDG/ghotto).
func installReleaseBinary(repo, bin string) error {
	asset := fmt.Sprintf("%s_%s_%s.tar.gz", bin, runtime.GOOS, runtime.GOARCH)
	url := fmt.Sprintf("https://github.com/%s/releases/latest/download/%s", repo, asset)

	tmpDir, err := os.MkdirTemp("", bin+"-install-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	tarball := filepath.Join(tmpDir, asset)
	if err := exec.Command("curl", "-fsSL", "-o", tarball, url).Run(); err != nil {
		return fmt.Errorf("download %s: %w", bin, err)
	}
	if err := exec.Command("tar", "-xzf", tarball, "-C", tmpDir).Run(); err != nil {
		return fmt.Errorf("extract %s: %w", bin, err)
	}

	binDir := filepath.Join(platform.HomeDir(), ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	dst := filepath.Join(binDir, bin)
	if err := exec.Command("cp", filepath.Join(tmpDir, bin), dst).Run(); err != nil {
		return fmt.Errorf("install %s binary: %w", bin, err)
	}
	return os.Chmod(dst, 0o755)
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
	// account. With no resolvable user (e.g. root in a minimal container),
	// fall back to a no-username chsh, which changes the calling account.
	if user == "" {
		return runner.Sudo("chsh", "-s", zshBin).Run()
	}
	return runner.Sudo("chsh", "-s", zshBin, user).Run()
}

// defaultShellIsZsh reports whether the target user's login shell is already
// the zsh setDefaultShell would choose.
func defaultShellIsZsh() bool {
	zshBin := findZsh()
	return zshBin != "" && currentLoginShell(targetUser()) == zshBin
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
