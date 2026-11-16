# Coding rules

Rules for writing, reviewing and planning code in any repo. A repo's own AGENTS.md adds to these and wins where they
conflict.

## Design

- Build few clear pieces, each with one job. A new source, route or tool should be a small addition, not a rework.
- Put logic in the module that owns the concept. Don't reach into another service's tables or internals; route files
  only route.
- Keep core code generic. Behavior specific to a platform or flow (Slack, a "seal" message) goes behind an interface,
  and lower layers never know which caller they serve.
- Replace growing `if kind === …` chains with an abstraction: an interface, a pipeline of steps, or a lookup. Prefer one
  generic accessor over a special case.
- Generalize structure, not features. Split out a reusable piece once its shape is clear, even with one caller. Don't
  build speculative features, edge cases or options.
- Reuse existing clients, components, paths and patterns. Parallel flows share one code path. Merge near-duplicate
  concepts, methods and files.
- Fix the system, not the symptom. No band-aids or leftover heuristics.
- Make clean cuts. Unshipped code gets no migrations, shims or dual-runtime layers. Delete superseded code,
  migration-only functions and aliases.
- Remove checks and fallbacks for states that can't happen, everywhere they appear. If a field can't be missing, make it
  required.
- Don't fail silently. Handle non-OK branches, throw on invalid input, and tell the user when something isn't
  supported.
- Keep the hot path lean: no per-request checks and no awaited side work. Move work to write or ingest time.
- Prefer deterministic logic to extra LLM calls. In agent code, give the model the outcome and let it decide what's
  next.
- Put large changes behind a feature flag.
- Before designing, look up how others solve the problem. When taking from another branch, port the concepts into the
  existing architecture rather than copying the code.

## Code style

- Name a thing for what it is (`taskStore`, not `tasks`). Use one name per concept. Rename anything misleading,
  colliding or vendor-specific.
- No nested ternaries. For more than two branches, use early returns, a `switch`, a lookup or a small function; in JSX,
  a small component.
- Extract complex inline logic into a named function or variable.
- No conditional spreads (`...(cond && {...})`) and no `for (;;)`. Use plain bounded loops.
- Give parameter shapes a named type, define each type once, and avoid stacked utility types
  (`Partial<Omit<…>> & {…}`).
- When code is called confusing, make it simpler, not cleverer.

## Tests

- Write few, concise tests that pin behavior a change could break.
- Run only the test files you need, never whole suites.

## Scope and workflow

- Do exactly the subset asked and defer the rest. Keep separate issues separate.
- Plan before implementing (in the artifact if there is one). Offer numbered options with a recommendation.
- Check facts in code or infra before stating them.
- Verify for real: dev server, CLI, evals or staging. QA acts like a user, not a code reader.
- Don't disturb running environments: worktrees with Tilt or Codex running, and no `bun run build` while Tilt is up.
- Change alerts, flags or secrets only when asked, and add rather than edit. Never print or commit secrets.
- When the user states a rule, write it into the repo's AGENTS.md and Greptile rules. Enforce it with lint where
  possible.

## Commits and PRs

- Stop for review before each logical commit unless told otherwise. Commit finished work before starting the next
  change.
- One concern per commit. Lint fixes go separately, and risky changes go in their own commit so they're easy to revert.
- One concern per PR. Keep diffs small, and simplify or split when one balloons. Fix a problem in the PR that
  introduced it.
- Stack with `gh stack`. Add new PRs on top, never mid-stack.
- Write PR descriptions with the `pr` skill: short, tables of single sentences, before/after numbers. Update the
  description after a push that changes scope.
- After opening a PR, keep going until CI is green (type check and code quality) and Greptile gives 5/5 with threads
  resolved. Skip comments that are unreasonable.
- Give the URL of every PR or issue you create.

## UI

- Research common layouts first, then build with the app's own components. Base components stay generic and are
  configured through parameters.
- No layout shift. Fixed bars keep a fixed height.
- Show only meaningful data. Drop numbers that are always zero, and show comparison detail only when comparing.
