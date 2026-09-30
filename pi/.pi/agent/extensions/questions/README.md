# Po questions

A local, model-callable `ask_user` tool. Non-blocking by default: it posts a persistent widget above the editor and immediately returns so Po can continue independent work. It does not take keyboard focus or send desktop notifications.

## Use

- `/answer` — open pending questions together in a tabbed dialog (a single question keeps the simpler UI). Typing focuses the editable final row and preserves the first character. Choices stay visible; ↑/↓ moves between choices and your draft.
- With multiple questions, ←/→ or Tab/Shift+Tab moves between question tabs, even while typing. Drafts and selected answers are preserved. Enter confirms a choice or draft and advances. The final **Submit** tab shows a summary and only sends once every question has an answer. Nothing is sent before that final Enter. Nonempty typed drafts count as answers; predefined choices require Enter to confirm.
- `/answer <id>` — open the group on that question's tab.
- `/answer <id> <text>` — answer directly (text is literal, not an option number).
- `/answer <id> --cancel` — explicitly cancel; Po receives a message that no approval was given.
- Escape — close the answer UI without submitting; all questions remain pending. Unsubmitted dialog drafts are discarded.

The tabbed group includes questions pending when `/answer` opens. New questions posted while it is open stay pending for the next dialog.

Answers arrive as user steering messages at the runtime's next safe boundary; they do not interrupt a running tool. If Po is idle, answering starts a new turn. Once independent work is exhausted, Po should stop rather than guess an answer. This is conversational guidance, not a tool-permission enforcement system.

`ask_user({ question, options?, blocking?: true })` instead waits in a dialog and returns the answer directly as a tool result. Escape/abort returns no approval. Blocking dialogs are not persisted; non-blocking pending questions are restored from the active session branch after reload/resume. The tool requires TUI mode and rejects print/JSON/RPC use.

## Install

Link this directory into `~/.pi/agent/extensions/questions`, then run `/reload` or start a new Po session. Only `index.ts` is loaded as the extension entry point.

## Checks

```sh
NODE_PATH="$HOME/.bun/install/global/node_modules" bun test ./pi/.pi/agent/extensions/questions/
```

Manual terminal checks (regular and fullscreen):

1. Ask Po to ask a non-blocking question with choices and continue inspecting files. Verify the question stays above the editor without taking focus while tools continue.
2. Run `/answer`, choose an option, and verify Po incorporates it and the widget clears.
3. Move to “Type an answer…” and start typing without pressing Enter first. Verify the first character is kept, the choices remain visible, and Enter submits. Use ↑/↓ to leave and return to the draft; verify it is preserved and Enter on a predefined choice submits that choice instead. Also type directly from another choice, paste text, and use `/answer <id> <text>`; check answers during both active work and idle time.
4. Post two questions and run `/answer`. Verify the tab bar, ←/→ navigation while typing, draft preservation, and Enter advancing to the next question. Visit Submit with a missing answer and verify it cannot send. Finish both, review the summary, and submit: Po should receive one message containing both answers. Repeat with Escape and verify neither question clears. Direct `/answer <id> <text>` and `--cancel` should still affect only that question.
5. Leave a question pending, `/reload`, and answer it. Switch sessions or navigate the session tree and verify questions do not leak between branches/sessions.
6. Request a blocking question. Check that Po waits; answer it, then repeat and press Escape. Dismissal must not count as approval.
7. Resize to a narrow terminal and try long questions, Unicode, and a theme switch. Verify widget wrapping and input focus remain usable.
