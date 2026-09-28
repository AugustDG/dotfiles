# Po session messages

Send messages between local Po sessions, including sessions in different workspaces.

## Usage

- `/peers` — list other active Po instances only.
- `/send <8-character-id> <message>` — send a message.
- `/send "exact session name" <message>` — target a named session.
- Ask Po naturally to list or message another session; Po can use the `session_message` tool.

Active instances receive messages within about one second. Inactive sessions are excluded from both discovery and message targeting, including targeting by ID. Saved sessions are untouched and remain available in `/resume`. Presence requires a live process and a heartbeat within the last 60 seconds; stale presence files are cleaned up during discovery. If a recipient exits after a message is queued, that already-queued message can still be delivered on resume. Delivery is at least once: after a process crash during receipt, a message may be delivered again.

## Session identities and sent messages

Each session gets a separate, persistent Po identity from 148 first names and 31 adjectives, such as `Christina Po`, `Curious Po`, or `Vindicating Po`. The identity is stored as extension metadata, not as the session title. `/name` changes only the task/session title; it never changes the Po identity or its tone. Adjective names add a subtle tone preference, never overriding accuracy, safety, or your instructions. First-name identities do not change tone.

Session listings show names first and retain 8-character IDs. Duplicate names require targeting by ID.

Sent tool calls reveal their full message when expanded (click in fullscreen mode, or use the tool-expansion shortcut). `/send` also leaves a persistent expandable sent-message card, without adding duplicate content to the model context.

Manual checks after `/reload`:
- Open a new Po session and check its identity in the editor border and another session's `/peers`.
- Send by name using both the tool and `/send`; click to expand/collapse the outgoing message.
- Resume the sender and verify its name and outgoing messages remain.
- Use `/name My task` and verify the Po identity in the editor border stays unchanged.

## Storage

Mailboxes and heartbeat files are local to `~/.pi/agent/session-messages/` (or `$PI_CODING_AGENT_DIR/session-messages/`). Successfully delivered mailbox files are deleted; the sender's tool call/result (or `/send` transcript entry) and recipient's injected custom message remain in their normal session histories.
