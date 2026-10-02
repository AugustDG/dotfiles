# Managed background shell tasks

Background builds, tests, servers, and log streams owned by one Po/Pi session.

## Enable

This directory is linked from `~/.pi/agent/extensions/background-tasks`. Run `/reload` or start a new Po session after installing/updating it.

## Agent tools

- `task_start({ command, cwd?, label?, notify? })`: start immediately and return an ID. `notify` defaults to `true`; set it to `false` for servers and log streams.
- `task_list({})`: show this session's active jobs and jobs finished within the last minute. Older jobs remain accessible by ID.
- `task_output({ id, tail_chars? })`: inspect status and the latest output. Default snippet: 500 characters, up to 8 lines. Request a larger tail explicitly, up to 16,000 characters.
- `task_wait({ id, timeout_seconds? })`: wait at most 60 seconds (default 10). Timeout returns current status. Cancelling the wait does **not** stop the job.
- `task_stop({ id })`: terminate the process group, first with SIGTERM, then SIGKILL after one second if needed.

Commands run through `/bin/bash -c`, with inherited environment, the session working directory by default, and no stdin. Relative `cwd` values resolve against the session directory. Use foreground commands: do not add `&`, daemonize, or detach children. Commands needing interactive input/TTY are not supported.

Task displays show a one-line command snippet (up to 160 characters), a short output snippet, and exit status plus readable duration, without a directory line. Finite jobs send one completion message with this compact summary; the message queues a follow-up when the agent is busy or wakes it when idle. `notify: false` suppresses that message. Starting jobs or reading their output does not send notifications. Shutdown suppresses completion turns.

## User controls

- `/tasks`: select an active or recently finished job, inspect its status/output, then refresh or stop it. Finished jobs disappear from the list after one minute.
- `/tasks <id>`: inspect one job.
- `/tasks stop <id>`: stop one job directly.
- The bottom bar shows `1 task` / `2 tasks` while jobs are running, and clears when none remain.

## Lifecycle and limits

- macOS/Linux only. Up to 8 concurrent jobs and 100 retained job records.
- Combined stdout/stderr is a bounded 256 Ki-character tail in memory. Tool/UI output strips terminal controls. There is **no full log archive**; redirect to a file yourself when you need one.
- Session entries save start/completion metadata and up to 16,000 output characters. Command/output history may contain sensitive information, just like ordinary shell tool results.
- Orderly quit, reload, new session, resume, and fork stop active process groups. A naturally exiting shell also has leftover descendants terminated.
- Restore shows prior records; previously active jobs become `interrupted`. Saved PIDs are never reused or signalled. Restored output may be shorter than the live tail.
- Jobs belong to the physical session, not a conversation tree branch. Tree navigation does not rewind real processes.
- Abrupt termination of Pi (SIGKILL, crash, power loss) cannot run cleanup. Descendants that deliberately detach into a different process group are outside this extension's control. This is lifecycle management, not a sandbox.

## Validation

From the dotfiles repository:

```sh
NODE_PATH="$HOME/.bun/install/global/node_modules" \
  node --experimental-strip-types --test \
  pi/.pi/agent/extensions/background-tasks/*.test.ts
```

Tests cover manager behavior and the extension through Pi's Jiti loader/dependencies and a stub API. They do not exercise the real terminal UI or a live model turn.

Manual smoke test:

1. Run `/reload`.
2. Ask Po: “Start `sleep 10; printf 'finished\\n'` as a background task, label it smoke, then continue without waiting.” Open `/tasks` while it runs; expect a completion event when it finishes.
3. Ask Po to start `sleep 300` with `notify=false`. Use `/tasks` to inspect and stop it; expect `stopped` and no completion turn.
4. Start another `sleep 300`, then `/reload`. `/tasks` should show it as stopped, not still running.
5. Inspect `/tasks` in a narrow terminal and with multiline/color output; verify selection, readability, cancel, and refresh behavior.
