---
name: code-review
description: Review local changes, a branch diff, or a pull request for concrete bugs, regressions, security risks, and missing tests. Use when the user asks for a concise code review or PR review.
---

# Code Review

Perform a read-only, findings-first review.

## Scope

1. Use the target named by the user.
2. For a PR, inspect its metadata and complete diff with the provider CLI.
3. Otherwise review uncommitted changes when present; if none, compare the current branch with its base.
4. Read applicable repository instructions and consult `.repowise` first when available.

## Review

- Read changed code with enough surrounding context to verify behavior.
- Focus on correctness, regressions, security, data loss, concurrency, API compatibility, and meaningful test gaps.
- Report only issues introduced or worsened by the change.
- Skip formatter/linter issues, subjective style preferences, and speculative refactors.
- Verify each finding before reporting it. Do not invent findings to fill the response.
- Do not modify code or post comments unless the user explicitly asks.

## Output

List findings first, ordered by severity:

- **High | Medium | Low** — `path:line` — concise problem statement, impact, and smallest reasonable fix.

Then add:

- **Questions** only when needed to establish correctness.
- **Summary** in at most three bullets, including validation gaps.

If there are no actionable findings, say so directly and mention any tests or evidence you could not verify.
