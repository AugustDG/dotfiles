package stow

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/AugustDG/dotfiles/internal/runner"
)

// ConflictError reports that stow could not link a module because one or more
// target paths already exist as real files (not stow-owned symlinks). It
// carries the conflicting targets parsed from stow's output so callers can
// explain the situation instead of surfacing a bare "exit status 1".
type ConflictError struct {
	Module  string
	Targets []string // conflicting paths, relative to the stow target (home) dir
	Output  string   // raw stow stderr, for verbose/debugging
}

func (e *ConflictError) Error() string {
	switch n := len(e.Targets); {
	case n == 0:
		return fmt.Sprintf("%s conflicts with existing files", e.Module)
	case n == 1:
		return fmt.Sprintf("~/%s already exists and is not a stow link", e.Targets[0])
	default:
		if p := commonParent(e.Targets); p != "" {
			return fmt.Sprintf("~/%s already has %d files not managed by stow", p, n)
		}
		return fmt.Sprintf("%d existing files conflict (e.g. ~/%s)", n, e.Targets[0])
	}
}

// commonParent returns the deepest directory shared by all paths (slash-joined,
// component-wise), or "" when they share no leading component.
func commonParent(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	prefix := strings.Split(filepath.ToSlash(paths[0]), "/")
	for _, p := range paths[1:] {
		parts := strings.Split(filepath.ToSlash(p), "/")
		end := len(prefix)
		if len(parts) < end {
			end = len(parts)
		}
		i := 0
		for i < end && prefix[i] == parts[i] {
			i++
		}
		prefix = prefix[:i]
		if len(prefix) == 0 {
			return ""
		}
	}
	// Drop a trailing basename when every path is the same single file.
	return strings.Join(prefix, "/")
}

// parseConflictTargets extracts the conflicting target paths from stow's
// stderr. stow phrases conflicts a few different ways; cover the common ones.
func parseConflictTargets(stowOutput string) []string {
	// More-specific phrasings must precede the generic "over existing target "
	// so they win the match. stow emits the directory/non-directory variants
	// without a trailing " since " clause; the "different package" variant ends
	// with " => <existing link dest>".
	markers := []string{
		"over existing directory target ",
		"over existing non-directory target ",
		"over existing target ",
		"existing target is not owned by stow: ",
		"existing target is neither a link nor a directory: ",
		"existing target is stowed to a different package: ",
	}
	seen := map[string]bool{}
	var targets []string
	for _, line := range strings.Split(stowOutput, "\n") {
		line = strings.TrimSpace(line)
		for _, m := range markers {
			i := strings.Index(line, m)
			if i < 0 {
				continue
			}
			t := line[i+len(m):]
			if j := strings.Index(t, " since "); j >= 0 {
				t = t[:j]
			}
			if j := strings.Index(t, " => "); j >= 0 {
				t = t[:j]
			}
			t = strings.TrimSpace(t)
			if t != "" && !seen[t] {
				seen[t] = true
				targets = append(targets, t)
			}
			break
		}
	}
	return targets
}

// skipFiles lists filenames that should be ignored when checking stow status.
var skipFiles = map[string]bool{
	"module.toml": true,
	".git":        true,
	".gitmodules": true,
	".gitignore":  true,
	"README.md":   true,
	"LICENSE":     true,
}

// IsStowed returns true if every leaf file in the module directory (excluding
// metadata files) has a corresponding symlink under homeDir pointing back to
// the dotfiles source.
func IsStowed(dotfilesDir, moduleName, homeDir string) bool {
	moduleDir := filepath.Join(dotfilesDir, moduleName)

	allLinked := true
	hasFiles := false

	filepath.Walk(moduleDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// A file that vanished between the directory read and the stat (e.g. a
			// runtime temp file in a config dir) is not a stable leaf — skip it
			// rather than misreporting the whole module as not stowed.
			if os.IsNotExist(err) {
				return nil
			}
			allLinked = false
			return nil
		}

		// Get the path relative to the module directory.
		rel, err := filepath.Rel(moduleDir, path)
		if err != nil {
			allLinked = false
			return nil
		}

		// Skip root directory itself.
		if rel == "." {
			return nil
		}

		// Skip metadata files and directories.
		base := filepath.Base(path)
		if skipFiles[base] {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// We only check leaf files.
		if info.IsDir() {
			return nil
		}

		hasFiles = true
		targetPath := filepath.Join(homeDir, rel)

		absSource, _ := filepath.Abs(path)

		if !isLinkedToSource(targetPath, absSource) {
			allLinked = false
		}

		return nil
	})

	return hasFiles && allLinked
}

// isLinkedToSource checks whether targetPath resolves to absSource, either via
// a direct symlink on the file itself or via a symlinked parent directory.
func isLinkedToSource(targetPath, absSource string) bool {
	// Fast path: the file itself is a symlink pointing straight at the source.
	if linkDest, err := os.Readlink(targetPath); err == nil {
		if !filepath.IsAbs(linkDest) {
			linkDest = filepath.Join(filepath.Dir(targetPath), linkDest)
		}
		if absLink, _ := filepath.Abs(linkDest); absLink == absSource {
			return true
		}
		// Not a direct match — fall through. The target may be a tracked symlink
		// (a symlink the module itself stows), or its parent may be a folded
		// directory; both are resolved correctly by comparing real paths below.
	}

	// General path: compare the fully resolved real path of both sides so a
	// folded parent directory (whole module stowed as one symlink), a tracked
	// symlink leaf, or an ancestor symlink (e.g. macOS /var -> /private/var)
	// don't cause false misses.
	resolvedTarget, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		return false
	}
	resolvedSource, err := filepath.EvalSymlinks(absSource)
	if err != nil {
		return false
	}
	return resolvedTarget == resolvedSource
}

// Stow runs GNU stow to create symlinks for the given module.
func Stow(dotfilesDir, moduleName, homeDir string) error {
	return runStow(dotfilesDir, moduleName, homeDir, "-R")
}

// StowAdopt runs GNU stow with --adopt: any target that already exists as a
// real file is moved into the package (replacing the package's copy) and then
// replaced by a symlink. This is stow's canonical resolver for "the files are
// already in place" — afterwards the repo's working tree shows the adopted
// content as a diff to review.
func StowAdopt(dotfilesDir, moduleName, homeDir string) error {
	return runStow(dotfilesDir, moduleName, homeDir, "-R", "--adopt")
}

// Unstow runs GNU stow to remove symlinks for the given module.
func Unstow(dotfilesDir, moduleName, homeDir string) error {
	return runStow(dotfilesDir, moduleName, homeDir, "-D")
}

// runStow invokes stow with the given mode flags, capturing stderr so failures
// can be explained. A conflict with pre-existing files yields a *ConflictError.
func runStow(dotfilesDir, moduleName, homeDir string, modeArgs ...string) error {
	args := append([]string{"-d", dotfilesDir, "-t", homeDir, "--ignore=module.toml"}, modeArgs...)
	args = append(args, moduleName)
	cmd := exec.Command("stow", args...)

	// Reuse the shared wiring, then tee stderr into a buffer regardless of
	// verbosity so the error message can carry stow's explanation.
	runner.ConfigureCmd(cmd)
	var stderr bytes.Buffer
	if runner.Verbose {
		cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	} else {
		cmd.Stderr = &stderr
	}

	if err := cmd.Run(); err != nil {
		out := stderr.String()
		if targets := parseConflictTargets(out); len(targets) > 0 {
			return &ConflictError{Module: moduleName, Targets: targets, Output: out}
		}
		if msg := firstLine(out); msg != "" {
			return fmt.Errorf("stow %s: %s", moduleName, msg)
		}
		return fmt.Errorf("stow %s: %w", moduleName, err)
	}
	return nil
}

// firstLine returns the first non-empty, trimmed line of s.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}
