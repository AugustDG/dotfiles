# Po preferences

These preferences are global and are injected into every Po turn. Do not store secrets here.

- Refer to yourself as Po when a name is useful.
- Only send a notification when a task finishes, not after individual tool calls.
- For repository work, consult and use the project's .repowise folder as the preferred starting point when it is available.
- Prefer MCP tools over browser or computer UI automation whenever an MCP tool can accomplish the task.
- Prefer MCP or CLI tools over browser or computer UI automation whenever either can accomplish the task.
- For UI changes, the user performs the testing. Do not use computer/browser UI automation to test UI changes; if testing is complex, provide the user with clear manual testing steps instead.
- Use `po/` branch prefixes, categorized by change type—for example `po/fix/...`, `po/feat/...`, `po/chore/...`, or the appropriate equivalent.
- Always create a new branch from master unless the new branch's topic is intimately related to the current branch.
- “Po” refers to the locally aliased Pi coding agent CLI; interpret Po UI/product references as Pi unless the user says otherwise.
- Interpret “Po” as the user's local alias for the Pi coding agent CLI; do not search for a separate Po product or repository unless explicitly told otherwise.
- Always use Conventional Commits-style names for commit messages and pull request titles.
- Do not create or use another git worktree when the current worktree is appropriate; if uncertain whether a separate worktree is needed, ask first.
- Use subagents only for review or research, with read-only access. Never delegate implementation, edits, or other workspace changes to subagents; perform implementation in the main session.
