package stow

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// heavyDirs are skipped when scanning for dangling links: they are never stow
// targets and can be enormous.
var heavyDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	".cache":       true,
}

// heavyTopLevel names $HOME subtrees that are far too large to walk wholesale
// (e.g. macOS ~/Library has millions of files). For modules that map into one,
// only the leaf's exact directory is scanned rather than the whole top level.
var heavyTopLevel = map[string]bool{
	"Library": true,
}

// DanglingLink is a broken symlink under $HOME that points into the dotfiles
// repo — typically left behind when a file is removed from a module.
type DanglingLink struct {
	Path   string // the symlink location
	Target string // where it points (now missing)
}

// ModuleLeaves returns the module-relative paths of every leaf file in a
// module directory, excluding metadata files. These are exactly the paths stow
// maps into $HOME.
func ModuleLeaves(dotfilesDir, moduleName string) []string {
	moduleDir := filepath.Join(dotfilesDir, moduleName)
	var leaves []string

	filepath.Walk(moduleDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		base := filepath.Base(path)
		if skipFiles[base] {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if rel, err := filepath.Rel(moduleDir, path); err == nil {
			leaves = append(leaves, rel)
		}
		return nil
	})

	return leaves
}

// ScanRoots returns the directories to search recursively for dangling links.
// It always includes ~/.config (the canonical config root) plus, for each
// module leaf, the top-level $HOME entry it maps into (e.g. ~/.claude, ~/.i3) —
// except for heavy top levels like ~/Library, where only the leaf's own
// directory is scanned to avoid walking millions of unrelated files. The
// shallow $HOME sweep in FindDangling catches top-level dotfile links, so this
// need not enumerate ~/.gitconfig and friends.
//
// Note: a link left after an entire top-level subtree is removed from a module
// (so no surviving leaf references it) is only caught if that top level is
// ~/.config or a direct child of $HOME; `clean` is best-effort maintenance.
func ScanRoots(dotfilesDir, homeDir string, moduleNames []string) []string {
	seen := make(map[string]bool)
	var roots []string
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			roots = append(roots, p)
		}
	}

	add(filepath.Join(homeDir, ".config"))
	for _, name := range moduleNames {
		for _, rel := range ModuleLeaves(dotfilesDir, name) {
			top := rel
			if i := strings.IndexRune(rel, filepath.Separator); i >= 0 {
				top = rel[:i]
			}
			if heavyTopLevel[top] {
				if dir := filepath.Dir(rel); dir != "." {
					add(filepath.Join(homeDir, dir))
				}
				continue
			}
			add(filepath.Join(homeDir, top))
		}
	}
	return roots
}

// FindDangling returns every symlink whose target resolves inside dotfilesDir
// but no longer exists. It sweeps the direct children of homeDir (so top-level
// dotfile links are always caught, even after their source is removed) and
// walks each root recursively. Symlinked directories are never descended into
// (WalkDir does not follow symlinks), so cycles and the repo itself are never
// traversed.
func FindDangling(dotfilesDir, homeDir string, roots []string) []DanglingLink {
	absRepo, _ := filepath.Abs(dotfilesDir)
	var found []DanglingLink
	seen := make(map[string]bool)

	record := func(path string) {
		if target, ok := danglingInto(path, absRepo); ok && !seen[path] {
			seen[path] = true
			found = append(found, DanglingLink{Path: path, Target: target})
		}
	}

	// Shallow sweep of $HOME's direct children.
	if entries, err := os.ReadDir(homeDir); err == nil {
		for _, e := range entries {
			if e.Type()&fs.ModeSymlink != 0 {
				record(filepath.Join(homeDir, e.Name()))
			}
		}
	}

	// Recursive walk of each root.
	for _, root := range roots {
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && heavyDirs[d.Name()] {
				return filepath.SkipDir
			}
			if d.Type()&fs.ModeSymlink != 0 {
				record(path)
			}
			return nil
		})
	}
	return found
}

// danglingInto reports whether linkPath is a broken symlink whose target lies
// within absRepo, returning the resolved (missing) target.
func danglingInto(linkPath, absRepo string) (string, bool) {
	dest, err := os.Readlink(linkPath)
	if err != nil {
		return "", false
	}
	if !filepath.IsAbs(dest) {
		dest = filepath.Join(filepath.Dir(linkPath), dest)
	}
	dest = filepath.Clean(dest)

	if !within(dest, absRepo) {
		return "", false
	}
	// A link that resolves cleanly is healthy, not dangling.
	if _, err := os.Stat(linkPath); err == nil {
		return "", false
	}
	return dest, true
}

// within reports whether path is equal to or nested under dir.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// RemoveDangling deletes the given symlinks, returning the paths removed and
// any error encountered (stopping at the first failure).
func RemoveDangling(links []DanglingLink) ([]string, error) {
	var removed []string
	for _, l := range links {
		if err := os.Remove(l.Path); err != nil {
			return removed, err
		}
		removed = append(removed, l.Path)
	}
	return removed, nil
}
