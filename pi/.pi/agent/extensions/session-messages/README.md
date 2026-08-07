# Po session messages

Send messages between local Po sessions, including sessions in different workspaces.

## Usage

- `/peers` — list sessions and live/offline status.
- `/send <8-character-id> <message>` — send a message.
- `/send "exact session name" <message>` — target a named session.
- Ask Po naturally to list or message another session; Po can use the `session_message` tool.

Active sessions receive messages within about one second. Messages for offline sessions remain queued until that session is resumed. Delivery is at least once: after a process crash during receipt, a message may be delivered again.

## Storage

Mailboxes and heartbeat files are local to `~/.pi/agent/session-messages/` (or `$PI_CODING_AGENT_DIR/session-messages/`). Successfully delivered mailbox files are deleted; both the sender's tool result and recipient's injected custom message remain in their normal session histories.
