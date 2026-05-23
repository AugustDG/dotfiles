package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/term"
)

// DetectOS returns the current operating system identifier ("darwin" or "linux").
func DetectOS() string {
	return runtime.GOOS
}

// HomeDir returns the current user's home directory.
func HomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// ExpandHome replaces a leading ~ with the user's home directory.
func ExpandHome(path string) string {
	if path == "~" {
		return HomeDir()
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(HomeDir(), path[2:])
	}
	return path
}

// DotfilesDir returns the dotfiles directory from the DOTFILES_DIR env var,
// falling back to ~/projects/dotfiles.
func DotfilesDir() string {
	if dir := os.Getenv("DOTFILES_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(HomeDir(), "projects", "dotfiles")
}

// IsInteractive returns true if stdin is connected to a terminal.
func IsInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// HasTTY returns true if /dev/tty is available (a real terminal exists even
// when stdin is piped, e.g. curl | bash).
func HasTTY() bool {
	f, err := os.Open("/dev/tty")
	if err != nil {
		return false
	}
	f.Close()
	return true
}
