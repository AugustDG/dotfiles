# Subagents extension

Delegates bounded, independent work to isolated Pi subprocesses. It is intended for parallel reconnaissance, repetitive checks, and low-effort/mechanical changes.

## Defaults

- Provider: `fireworks`
- Model: `accounts/fireworks/models/deepseek-v4-flash-0731`
- Access: `read-only`
- Thinking: `low` (overridable per invocation or task)
- Concurrency: 4 (maximum 4)
- Tasks per call: 1–8
- Timeout: 600 seconds per task (maximum 900)

Each child receives the normal project context (including applicable `AGENTS.md` files) plus a focused subagent prompt. Child tool allowlists exclude the `subagents` tool, preventing recursive delegation.

## Live activity UI

The `subagents` tool row is a live dashboard showing:

- queued, running, completed, and failed tasks;
- elapsed time and selected provider/model;
- the latest assistant note or tool call (`read`, `grep`, `bash`, etc.) for each task;
- timeout or provider errors.

Press Ctrl+O while it is running to expand the dashboard and see up to 15 recent activity entries per task. Collapse it again with Ctrl+O. This uses Pi's native tool UI, so it works without taking focus away from the editor.

## User model control

```text
/subagents-model
/subagents-model list
/subagents-model list fireworks
/subagents-model reset
/subagents-model fireworks/accounts/fireworks/models/deepseek-v4-flash-0731
/subagents-model anthropic/claude-haiku-4-5
```

The selected default is stored in the current Pi session and restored with that session.

## Model-controlled overrides

The `subagents` tool accepts:

- top-level `provider` and `model` defaults for one invocation;
- per-task `provider` and `model` overrides;
- `provider/model-id` in a model field, or a bare model ID when `provider` is supplied;
- invocation-level and per-task `thinking` overrides (`off` through `max`).

Every selected model must exist in Pi's model registry, and its provider must be authenticated.

## Access modes

- `read-only` (default): `read`, `grep`, `find`, `ls`
- `workspace`: `read`, `bash`, `edit`, `write`, `grep`, `find`, `ls`

Only parallelize `workspace` tasks when their files/directories do not overlap. Separate child processes cannot coordinate file mutation queues with one another.

## Example calls

Parallel read-only research using the session default:

```json
{
  "tasks": [
    { "task": "Find where model selection is implemented and report exact paths." },
    { "task": "Find tests covering provider authentication and summarize gaps." }
  ]
}
```

Mechanical edits on disjoint files with an explicit provider/model:

```json
{
  "provider": "fireworks",
  "model": "accounts/fireworks/models/deepseek-v4-flash-0731",
  "tasks": [
    { "task": "Fix formatting in src/a.ts and verify it.", "access": "workspace" },
    { "task": "Fix formatting in src/b.ts and verify it.", "access": "workspace" }
  ]
}
```

Per-task providers:

```json
{
  "tasks": [
    {
      "provider": "fireworks",
      "model": "accounts/fireworks/models/deepseek-v4-flash-0731",
      "task": "Perform fast code reconnaissance."
    },
    {
      "provider": "anthropic",
      "model": "claude-haiku-4-5",
      "task": "Independently check the same area for missed edge cases."
    }
  ]
}
```
