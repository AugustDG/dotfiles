package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AugustDG/dotfiles/internal/config"
	gitops "github.com/AugustDG/dotfiles/internal/git"
	"github.com/AugustDG/dotfiles/internal/platform"
	"github.com/AugustDG/dotfiles/internal/tui"
	"github.com/spf13/cobra"
)

type syncOptions struct {
	message string
	dryRun  bool
}

type syncPlan struct {
	submodulePaths []string
	stagePaths     []string
}

func syncCmd() *cobra.Command {
	opts := syncOptions{}

	cmd := &cobra.Command{
		Use:               "sync [modules...]",
		Short:             "Commit and push local changes, submodules first",
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: moduleNameCompletion,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSync(opts, args)
		},
	}

	cmd.Flags().StringVarP(&opts.message, "message", "m", "", `Commit message (default "Sync <repo>")`)
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Show what would be committed and pushed")
	return cmd
}

func runSync(opts syncOptions, args []string) error {
	dotfilesDir := platform.DotfilesDir()
	modules, err := config.DiscoverModules(dotfilesDir)
	if err != nil {
		return err
	}

	plan, err := planSync(dotfilesDir, modules, args)
	if err != nil {
		return err
	}

	anySynced := false
	for _, submodulePath := range plan.submodulePaths {
		synced, err := syncRepo(filepath.Join(dotfilesDir, submodulePath), submodulePath, opts.message, opts.dryRun)
		if err != nil {
			return err
		}
		anySynced = anySynced || synced
	}

	synced, err := syncRoot(dotfilesDir, plan.stagePaths, opts.message, opts.dryRun)
	if err != nil {
		return err
	}
	if !anySynced && !synced {
		tui.OK("Everything clean and pushed.")
	}
	return nil
}

func planSync(dotfilesDir string, modules []config.Module, args []string) (syncPlan, error) {
	if len(args) == 0 {
		return syncPlan{
			submodulePaths: gitops.Submodules(dotfilesDir),
			stagePaths:     []string{"-A"},
		}, nil
	}

	selected, err := resolveModuleArgs(modules, args)
	if err != nil {
		return syncPlan{}, err
	}

	plan := syncPlan{stagePaths: make([]string, 0, len(selected))}
	for _, mod := range selected {
		plan.submodulePaths = append(plan.submodulePaths, mod.SubmodulePaths...)
		plan.stagePaths = append(plan.stagePaths, mod.Name)
	}
	return plan, nil
}

// syncRepo commits and pushes the repo at path, recursing into its own
// submodules first so every pointer bump references an already-pushed commit.
// Returns whether anything was (or would be) committed or pushed.
func syncRepo(path, name, message string, dryRun bool) (bool, error) {
	synced := false
	for _, submodulePath := range gitops.Submodules(path) {
		subSynced, err := syncRepo(
			filepath.Join(path, submodulePath),
			name+"/"+submodulePath,
			message,
			dryRun,
		)
		if err != nil {
			return synced, err
		}
		synced = synced || subSynced
	}

	if gitops.IsDirty(path) {
		if _, err := gitops.CurrentBranch(path); err != nil {
			// Submodules are normally on a detached HEAD. If a local branch is
			// already at this commit, attach to it (no content moves) so the
			// commit has a home; otherwise the user must pick a branch.
			branch, ok := gitops.AttachableBranch(path)
			if !ok {
				return synced, fmt.Errorf("%s has changes on a detached HEAD that diverges from every local branch; check out a branch first", name)
			}
			if dryRun {
				tui.Info("%s: would attach detached HEAD to %s (fast-forward), then commit", name, branch)
			} else {
				if err := gitops.AttachBranch(path, branch); err != nil {
					return synced, fmt.Errorf("%s: attach to %s: %w", name, branch, err)
				}
				tui.OK("%s: attached detached HEAD to %s", name, branch)
			}
		}
		if dryRun {
			msg := message
			if msg == "" {
				msg = syncMessage(gitops.ChangedPaths(path))
			}
			tui.Info("%s: would commit (%s)", name, msg)
		} else {
			if err := gitops.Add(path, "-A"); err != nil {
				return synced, fmt.Errorf("%s: stage: %w", name, err)
			}
			msg := message
			if msg == "" {
				msg = syncMessage(gitops.StagedPaths(path))
			}
			if err := gitops.Commit(path, msg); err != nil {
				return synced, fmt.Errorf("%s: commit: %w", name, err)
			}
			tui.OK("%s: committed (%s)", name, msg)
		}
		synced = true
	}

	if gitops.HasUnpushed(path) {
		if dryRun {
			tui.Info("%s: would push", name)
		} else {
			if err := gitops.Push(path); err != nil {
				return synced, fmt.Errorf("%s: push: %w", name, err)
			}
			tui.OK("%s: pushed", name)
		}
		synced = true
	}

	return synced, nil
}

// syncRoot stages the given paths in the dotfiles repo, then commits and pushes.
func syncRoot(dotfilesDir string, stagePaths []string, message string, dryRun bool) (bool, error) {
	synced := false

	if gitops.IsDirty(dotfilesDir) {
		if _, err := gitops.CurrentBranch(dotfilesDir); err != nil {
			return false, fmt.Errorf("dotfiles repo has changes but is on a detached HEAD; check out a branch first")
		}
		if dryRun {
			msg := message
			if msg == "" {
				msg = syncMessage(gitops.ChangedPaths(dotfilesDir))
			}
			tui.Info("dotfiles: would commit (%s)", msg)
			synced = true
		} else {
			if err := gitops.Add(dotfilesDir, stagePaths...); err != nil {
				return false, fmt.Errorf("dotfiles: stage: %w", err)
			}
			if gitops.HasStaged(dotfilesDir) {
				msg := message
				if msg == "" {
					msg = syncMessage(gitops.StagedPaths(dotfilesDir))
				}
				if err := gitops.Commit(dotfilesDir, msg); err != nil {
					return false, fmt.Errorf("dotfiles: commit: %w", err)
				}
				tui.OK("dotfiles: committed (%s)", msg)
				synced = true
			}
		}
	}

	if gitops.HasUnpushed(dotfilesDir) {
		if dryRun {
			tui.Info("dotfiles: would push")
		} else {
			if err := gitops.Push(dotfilesDir); err != nil {
				return synced, fmt.Errorf("dotfiles: push: %w", err)
			}
			tui.OK("dotfiles: pushed")
		}
		synced = true
	}

	return synced, nil
}

// syncMessage builds the default commit message when the user passes no -m,
// summarising the committed paths by their top-level area — e.g.
// "sync: cmd, internal, zsh". Replaces the old generic "Sync dotfiles".
func syncMessage(paths []string) string {
	groups := topLevelGroups(paths)
	if len(groups) == 0 {
		return "sync"
	}
	return "sync: " + strings.Join(groups, ", ")
}

// topLevelGroups returns the unique first path segments (or the whole name for
// root-level files), sorted, capped with a "+N more" marker so the subject
// line stays short.
func topLevelGroups(paths []string) []string {
	const max = 6
	seen := make(map[string]bool, len(paths))
	var groups []string
	for _, p := range paths {
		seg := p
		if i := strings.IndexByte(p, '/'); i >= 0 {
			seg = p[:i]
		}
		if seg != "" && !seen[seg] {
			seen[seg] = true
			groups = append(groups, seg)
		}
	}
	sort.Strings(groups)
	if len(groups) > max {
		groups = append(groups[:max:max], fmt.Sprintf("+%d more", len(groups)-max))
	}
	return groups
}
