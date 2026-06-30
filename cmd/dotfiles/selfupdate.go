package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/AugustDG/dotfiles/internal/tui"
	"github.com/spf13/cobra"
)

// releaseRepo and releaseTag mirror install.sh: the CI "latest" prerelease
// carries one binary asset per platform.
const (
	releaseRepo = "AugustDG/dotfiles"
	releaseTag  = "latest"
)

func selfUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "self-update",
		Short: "Download and install the latest dotfiles CLI binary",
		Long: "Replaces the running dotfiles binary with the latest release build for\n" +
			"this OS and architecture. The currently-installed location is updated in\n" +
			"place.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSelfUpdate()
		},
	}
}

func runSelfUpdate() error {
	target, err := executablePath()
	if err != nil {
		return fmt.Errorf("locate current binary: %w", err)
	}

	asset := fmt.Sprintf("dotfiles-%s-%s", runtime.GOOS, runtime.GOARCH)
	url := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", releaseRepo, releaseTag, asset)

	errs, runErr := tui.RunTasks("Updating dotfiles CLI", []tui.Task{
		{
			Title: fmt.Sprintf("Download %s", asset),
			Run:   func(ctx context.Context) error { return downloadBinary(ctx, url, target) },
		},
	})
	if runErr != nil {
		return runErr
	}
	if err := firstError(errs); err != nil {
		return err
	}

	fmt.Printf("Installed to %s\n", target)
	if v := binaryVersion(target); v != "" {
		fmt.Printf("Now running %s\n", v)
	}
	return nil
}

// executablePath returns the absolute, symlink-resolved path of the running
// binary so the real file is replaced rather than a symlink to it.
func executablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved, nil
	}
	return exe, nil
}

// downloadBinary fetches url and atomically replaces target. It writes to a
// temp file in the same directory (so rename stays on one filesystem), verifies
// the download is complete, sets the executable bit, then renames over the
// target. The context lets an aborted run cancel the transfer before the
// running binary is replaced.
func downloadBinary(ctx context.Context, url, target string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}

	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".dotfiles-update-*")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed

	n, err := io.Copy(tmp, resp.Body)
	if err != nil {
		tmp.Close()
		return fmt.Errorf("write binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// Reject a truncated download (clean EOF, fewer bytes than advertised)
	// before it can be made executable and swapped over the live binary.
	if resp.ContentLength >= 0 && n != resp.ContentLength {
		return fmt.Errorf("download truncated: got %d of %d bytes", n, resp.ContentLength)
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return fmt.Errorf("replace %s (is it writable?): %w", target, err)
	}
	return nil
}

// binaryVersion runs `<path> --version` and returns the trimmed output.
func binaryVersion(path string) string {
	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
