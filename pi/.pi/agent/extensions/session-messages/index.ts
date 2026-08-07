import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { SessionManager } from "@earendil-works/pi-coding-agent";
import { StringEnum } from "@earendil-works/pi-ai";
import { Type } from "typebox";
import { randomUUID } from "node:crypto";
import { mkdir, readFile, readdir, rename, stat, unlink, writeFile } from "node:fs/promises";
import { homedir } from "node:os";
import { basename, join } from "node:path";

const AGENT_DIR = process.env.PI_CODING_AGENT_DIR ?? join(homedir(), ".pi", "agent");
const MAILBOX_DIR = join(AGENT_DIR, "session-messages");
const INBOX_DIR = join(MAILBOX_DIR, "inbox");
const PRESENCE_DIR = join(MAILBOX_DIR, "presence");
const POLL_INTERVAL_MS = 750;
const HEARTBEAT_INTERVAL_MS = 5_000;
const PRESENCE_STALE_MS = 60_000;
const CLAIM_STALE_MS = 60_000;
const MAX_MESSAGE_LENGTH = 10_000;
const MAX_LISTED_SESSIONS = 30;

interface Envelope {
	version: 1;
	id: string;
	fromSessionId: string;
	fromSessionName?: string;
	fromCwd: string;
	toSessionId: string;
	message: string;
	createdAt: string;
}

interface Presence {
	version: 1;
	sessionId: string;
	sessionFile?: string;
	name?: string;
	cwd: string;
	pid: number;
	startedAt: string;
	lastSeen: string;
}

interface CurrentSession {
	id: string;
	file?: string;
	sessionDir: string;
	name?: string;
	cwd: string;
	startedAt: string;
}

interface KnownSession {
	id: string;
	path?: string;
	cwd: string;
	name?: string;
	firstMessage?: string;
	modified: Date;
	active: boolean;
}

function errorMessage(error: unknown): string {
	return error instanceof Error ? error.message : String(error);
}

function isNotFound(error: unknown): boolean {
	return error instanceof Error && "code" in error && error.code === "ENOENT";
}

function isAlreadyExists(error: unknown): boolean {
	return error instanceof Error && "code" in error && error.code === "EEXIST";
}

function isSafeSessionId(value: string): boolean {
	return /^[A-Za-z0-9][A-Za-z0-9._-]{5,127}$/.test(value);
}

function compactId(id: string): string {
	return id.length <= 8 ? id : id.slice(-8);
}

function displayName(session: Pick<KnownSession, "name" | "firstMessage" | "cwd">): string {
	const value = session.name?.trim() || session.firstMessage?.trim() || basename(session.cwd) || "unnamed session";
	return value.length > 64 ? `${value.slice(0, 61).trimEnd()}…` : value;
}

function parseJson<T>(content: string): T {
	return JSON.parse(content) as T;
}

async function ensureDirectories(): Promise<void> {
	await Promise.all([
		mkdir(INBOX_DIR, { recursive: true, mode: 0o700 }),
		mkdir(PRESENCE_DIR, { recursive: true, mode: 0o700 }),
	]);
}

async function atomicWrite(path: string, content: string): Promise<void> {
	const temporary = `${path}.${process.pid}.${randomUUID()}.tmp`;
	await writeFile(temporary, content, { encoding: "utf8", mode: 0o600, flag: "wx" });
	try {
		await rename(temporary, path);
	} catch (error) {
		await unlink(temporary).catch(() => undefined);
		throw error;
	}
}

function presencePath(sessionId: string): string {
	return join(PRESENCE_DIR, `${sessionId}.${process.pid}.json`);
}

async function writePresence(session: CurrentSession): Promise<void> {
	const presence: Presence = {
		version: 1,
		sessionId: session.id,
		sessionFile: session.file,
		name: session.name,
		cwd: session.cwd,
		pid: process.pid,
		startedAt: session.startedAt,
		lastSeen: new Date().toISOString(),
	};
	await atomicWrite(presencePath(session.id), `${JSON.stringify(presence)}\n`);
}

function isProcessAlive(pid: number): boolean {
	if (!Number.isInteger(pid) || pid <= 0) return false;
	try {
		process.kill(pid, 0);
		return true;
	} catch (error) {
		return error instanceof Error && "code" in error && error.code === "EPERM";
	}
}

async function readActivePresence(): Promise<Presence[]> {
	await ensureDirectories();
	const now = Date.now();
	const active: Presence[] = [];
	const entries = await readdir(PRESENCE_DIR, { withFileTypes: true });
	await Promise.all(entries.map(async (entry) => {
		if (!entry.isFile() || !entry.name.endsWith(".json")) return;
		const path = join(PRESENCE_DIR, entry.name);
		try {
			const presence = parseJson<Presence>(await readFile(path, "utf8"));
			const lastSeen = Date.parse(presence.lastSeen);
			if (
				presence.version !== 1 ||
				!isSafeSessionId(presence.sessionId) ||
				!Number.isFinite(lastSeen) ||
				now - lastSeen > PRESENCE_STALE_MS ||
				!isProcessAlive(presence.pid)
			) {
				await unlink(path).catch(() => undefined);
				return;
			}
			active.push(presence);
		} catch (error) {
			if (!isNotFound(error)) await unlink(path).catch(() => undefined);
		}
	}));
	return active;
}

async function knownSessions(sessionDir?: string): Promise<KnownSession[]> {
	const [defaultStored, customStored, presence] = await Promise.all([
		SessionManager.listAll(),
		sessionDir ? SessionManager.listAll(sessionDir) : Promise.resolve([]),
		readActivePresence(),
	]);
	const stored = [...defaultStored, ...customStored];
	const activeIds = new Set(presence.map((item) => item.sessionId));
	const byId = new Map<string, KnownSession>();

	for (const session of stored) {
		byId.set(session.id, {
			id: session.id,
			path: session.path,
			cwd: session.cwd,
			name: session.name,
			firstMessage: session.firstMessage,
			modified: session.modified,
			active: activeIds.has(session.id),
		});
	}

	for (const item of presence) {
		const existing = byId.get(item.sessionId);
		if (existing) {
			existing.active = true;
			existing.name = item.name ?? existing.name;
			existing.cwd = item.cwd || existing.cwd;
			continue;
		}
		byId.set(item.sessionId, {
			id: item.sessionId,
			path: item.sessionFile,
			cwd: item.cwd,
			name: item.name,
			modified: new Date(item.lastSeen),
			active: true,
		});
	}

	return [...byId.values()].sort((a, b) => {
		if (a.active !== b.active) return a.active ? -1 : 1;
		return b.modified.getTime() - a.modified.getTime();
	});
}

function formatSessions(sessions: KnownSession[], currentSessionId: string): string {
	const peers = sessions.filter((session) => session.id !== currentSessionId).slice(0, MAX_LISTED_SESSIONS);
	if (peers.length === 0) return "No other Po sessions found.";
	const lines = peers.map((session) => {
		const status = session.active ? "active" : "offline";
		return `${compactId(session.id)}  [${status}]  ${displayName(session)}  —  ${session.cwd || "unknown cwd"}`;
	});
	const omitted = sessions.filter((session) => session.id !== currentSessionId).length - peers.length;
	if (omitted > 0) lines.push(`…and ${omitted} older session(s).`);
	return `Po sessions (target by the 8-character ID, exact name, or cwd):\n${lines.join("\n")}`;
}

function resolveTarget(target: string, sessions: KnownSession[], currentSessionId: string): KnownSession {
	const query = target.trim();
	if (!query) throw new Error("A target session is required. List sessions first.");
	const normalized = query.toLowerCase();
	const peers = sessions.filter((session) => session.id !== currentSessionId);
	let matches = peers.filter((session) => session.id.toLowerCase() === normalized || session.path?.toLowerCase() === normalized);

	if (matches.length === 0 && query.length >= 6) {
		matches = peers.filter((session) => {
			const id = session.id.toLowerCase();
			return id.startsWith(normalized) || id.endsWith(normalized);
		});
	}
	if (matches.length === 0) {
		matches = peers.filter((session) => session.name?.trim().toLowerCase() === normalized);
	}
	if (matches.length === 0) {
		matches = peers.filter((session) => {
			const cwd = session.cwd.toLowerCase();
			return cwd === normalized || basename(cwd) === normalized;
		});
	}
	if (matches.length === 0) throw new Error(`No Po session matches "${query}". List sessions and use its 8-character ID.`);
	if (matches.length > 1) {
		throw new Error(`"${query}" matches multiple sessions: ${matches.slice(0, 8).map((session) => compactId(session.id)).join(", ")}. Use an ID.`);
	}
	return matches[0]!;
}

async function queueMessage(from: CurrentSession, target: KnownSession, message: string): Promise<Envelope> {
	const text = message.trim();
	if (!text) throw new Error("Message cannot be empty.");
	if (text.length > MAX_MESSAGE_LENGTH) throw new Error(`Message must be ${MAX_MESSAGE_LENGTH.toLocaleString()} characters or fewer.`);
	if (!isSafeSessionId(from.id) || !isSafeSessionId(target.id)) throw new Error("Session ID is not safe for mailbox delivery.");

	const envelope: Envelope = {
		version: 1,
		id: randomUUID(),
		fromSessionId: from.id,
		fromSessionName: from.name,
		fromCwd: from.cwd,
		toSessionId: target.id,
		message: text,
		createdAt: new Date().toISOString(),
	};
	const inbox = join(INBOX_DIR, target.id);
	await mkdir(inbox, { recursive: true, mode: 0o700 });
	const filename = `${Date.now()}_${envelope.id}.json`;
	await atomicWrite(join(inbox, filename), `${JSON.stringify(envelope)}\n`);
	return envelope;
}

function validateEnvelope(value: Envelope, expectedSessionId: string): void {
	if (
		value.version !== 1 ||
		!value.id ||
		!isSafeSessionId(value.fromSessionId) ||
		typeof value.fromCwd !== "string" ||
		(value.fromSessionName !== undefined && typeof value.fromSessionName !== "string") ||
		value.toSessionId !== expectedSessionId ||
		typeof value.message !== "string" ||
		!value.message.trim() ||
		value.message.length > MAX_MESSAGE_LENGTH ||
		!Number.isFinite(Date.parse(value.createdAt))
	) {
		throw new Error("Invalid session message envelope.");
	}
}

function renderIncomingMessage(envelope: Envelope): string {
	const sender = envelope.fromSessionName?.trim() || basename(envelope.fromCwd) || "unnamed session";
	return [
		"Message from another Po session:",
		`From: ${sender} (${compactId(envelope.fromSessionId)})`,
		`Workspace: ${envelope.fromCwd}`,
		"",
		envelope.message,
		"",
		`If a reply is useful, send it with session_message to target ${compactId(envelope.fromSessionId)}.`,
	].join("\n");
}

async function recoverStaleClaims(inbox: string): Promise<void> {
	const entries = await readdir(inbox, { withFileTypes: true });
	await Promise.all(entries.map(async (entry) => {
		if (!entry.isFile() || !entry.name.includes(".json.claim-")) return;
		const claimedPath = join(inbox, entry.name);
		try {
			const info = await stat(claimedPath);
			if (Date.now() - info.mtimeMs <= CLAIM_STALE_MS) return;
			const originalName = entry.name.slice(0, entry.name.indexOf(".claim-"));
			await rename(claimedPath, join(inbox, originalName));
		} catch (error) {
			if (!isNotFound(error) && !isAlreadyExists(error)) throw error;
		}
	}));
}

async function drainInbox(session: CurrentSession, pi: ExtensionAPI, shouldContinue: () => boolean): Promise<number> {
	const inbox = join(INBOX_DIR, session.id);
	await mkdir(inbox, { recursive: true, mode: 0o700 });
	await recoverStaleClaims(inbox);
	const entries = (await readdir(inbox, { withFileTypes: true }))
		.filter((entry) => entry.isFile() && entry.name.endsWith(".json"))
		.map((entry) => entry.name)
		.sort();
	let delivered = 0;

	for (const name of entries) {
		if (!shouldContinue()) break;
		const originalPath = join(inbox, name);
		const claimedPath = `${originalPath}.claim-${Date.now()}-${process.pid}`;
		try {
			await rename(originalPath, claimedPath);
		} catch (error) {
			if (isNotFound(error)) continue;
			throw error;
		}

		let envelope: Envelope;
		let content: string;
		try {
			envelope = parseJson<Envelope>(await readFile(claimedPath, "utf8"));
			validateEnvelope(envelope, session.id);
			content = renderIncomingMessage(envelope);
		} catch {
			// Quarantine malformed local mailbox files so one bad envelope cannot block later messages.
			await rename(claimedPath, `${originalPath}.invalid-${Date.now()}`).catch(() => undefined);
			continue;
		}

		if (!shouldContinue()) {
			await rename(claimedPath, originalPath).catch(() => undefined);
			break;
		}

		try {
			pi.sendMessage(
				{
					customType: "po-session-message",
					content,
					display: true,
					details: envelope,
				},
				{ deliverAs: "followUp", triggerTurn: true },
			);
			await unlink(claimedPath);
			delivered += 1;
		} catch (error) {
			await rename(claimedPath, originalPath).catch(() => undefined);
			throw error;
		}
	}
	return delivered;
}

function stripMatchingQuotes(value: string): string {
	if (value.length < 2) return value;
	const quote = value[0];
	return (quote === '"' || quote === "'") && value.at(-1) === quote ? value.slice(1, -1) : value;
}

function parseSendCommand(args: string): { target: string; message: string } | undefined {
	const input = args.trim();
	if (!input) return undefined;
	if (input.startsWith('"') || input.startsWith("'")) {
		const quote = input[0]!;
		const end = input.indexOf(quote, 1);
		if (end < 0) return undefined;
		return { target: input.slice(1, end).trim(), message: stripMatchingQuotes(input.slice(end + 1).trim()) };
	}
	const separator = input.search(/\s/);
	if (separator < 0) return undefined;
	return { target: input.slice(0, separator), message: stripMatchingQuotes(input.slice(separator).trim()) };
}

function sessionFromContext(ctx: ExtensionContext, pi: ExtensionAPI): CurrentSession {
	return {
		id: ctx.sessionManager.getSessionId(),
		file: ctx.sessionManager.getSessionFile(),
		sessionDir: ctx.sessionManager.getSessionDir(),
		name: pi.getSessionName(),
		cwd: ctx.cwd,
		startedAt: new Date().toISOString(),
	};
}

export default function sessionMessages(pi: ExtensionAPI) {
	let current: CurrentSession | undefined;
	let pollTimer: ReturnType<typeof setInterval> | undefined;
	let heartbeatTimer: ReturnType<typeof setInterval> | undefined;
	let generation = 0;
	let draining = false;

	async function poll(expectedGeneration: number): Promise<void> {
		if (draining || expectedGeneration !== generation || !current) return;
		draining = true;
		const sessionId = current.id;
		try {
			await drainInbox(current, pi, () => expectedGeneration === generation && current?.id === sessionId);
		} catch (error) {
			// Keep polling: transient mailbox or session lifecycle failures should not stop delivery.
			void error;
		} finally {
			draining = false;
		}
	}

	async function send(targetValue: string, message: string): Promise<{ target: KnownSession; live: boolean }> {
		if (!current) throw new Error("The current Po session is not ready.");
		const sessions = await knownSessions(current.sessionDir);
		const target = resolveTarget(targetValue, sessions, current.id);
		await queueMessage(current, target, message);
		return { target, live: target.active };
	}

	pi.registerTool({
		name: "session_message",
		label: "Session message",
		description:
			"List other Po sessions or send a text message to one. Active sessions receive messages live; offline sessions receive queued messages when resumed.",
		promptSnippet: "List Po sessions and send messages between them",
		promptGuidelines: [
			"Use session_message when the user asks to list, contact, coordinate with, or send information to another Po session.",
		],
		parameters: Type.Object({
			action: StringEnum(["list", "send"] as const, { description: "Whether to list sessions or send a message" }),
			target: Type.Optional(Type.String({ description: "For send: an 8-character session ID, exact session name, or cwd" })),
			message: Type.Optional(Type.String({ description: `For send: message text, up to ${MAX_MESSAGE_LENGTH.toLocaleString()} characters` })),
		}),
		executionMode: "sequential",
		async execute(_toolCallId, params) {
			if (!current) throw new Error("The current Po session is not ready.");
			if (params.action === "list") {
				const text = formatSessions(await knownSessions(current.sessionDir), current.id);
				return {
					content: [{ type: "text", text }],
					details: { currentSessionId: current.id, targetSessionId: "", live: false },
				};
			}
			if (!params.target?.trim() || !params.message?.trim()) {
				throw new Error("Both target and message are required when action is send.");
			}
			const result = await send(params.target, params.message);
			const status = result.live ? "queued for live delivery" : "queued for delivery when that session resumes";
			return {
				content: [{ type: "text", text: `Message ${status}: ${displayName(result.target)} (${compactId(result.target.id)}).` }],
				details: { currentSessionId: current.id, targetSessionId: result.target.id, live: result.live },
			};
		},
	});

	pi.registerCommand("peers", {
		description: "List other Po sessions and their live/offline status",
		async handler(_args, ctx) {
			try {
				const session = current ?? sessionFromContext(ctx, pi);
				ctx.ui.notify(formatSessions(await knownSessions(session.sessionDir), session.id), "info");
			} catch (error) {
				ctx.ui.notify(`Could not list Po sessions: ${errorMessage(error)}`, "error");
			}
		},
	});

	pi.registerCommand("send", {
		description: "Send to another Po session: /send <session-id> <message>",
		async handler(args, ctx) {
			const parsed = parseSendCommand(args);
			if (!parsed?.target || !parsed.message) {
				ctx.ui.notify('Usage: /send <session-id> <message> (quote an exact name containing spaces)', "warning");
				return;
			}
			try {
				const result = await send(parsed.target, parsed.message);
				ctx.ui.notify(
					result.live
						? `Message queued for live delivery to ${displayName(result.target)} (${compactId(result.target.id)}).`
						: `Session is offline; message queued until ${displayName(result.target)} (${compactId(result.target.id)}) resumes.`,
					"info",
				);
			} catch (error) {
				ctx.ui.notify(`Could not send message: ${errorMessage(error)}`, "error");
			}
		},
	});

	pi.on("session_start", async (_event, ctx) => {
		generation += 1;
		const expectedGeneration = generation;
		const session = sessionFromContext(ctx, pi);
		current = session;

		pollTimer = setInterval(() => void poll(expectedGeneration), POLL_INTERVAL_MS);
		pollTimer.unref?.();
		heartbeatTimer = setInterval(() => {
			if (expectedGeneration === generation && current) void writePresence(current).catch(() => undefined);
		}, HEARTBEAT_INTERVAL_MS);
		heartbeatTimer.unref?.();

		try {
			await ensureDirectories();
			if (expectedGeneration === generation && current?.id === session.id) await writePresence(session);
		} catch (error) {
			if (expectedGeneration === generation) {
				ctx.ui.notify(`Po session messaging could not initialize yet: ${errorMessage(error)}`, "warning");
			}
		}
		void poll(expectedGeneration);
	});

	pi.on("session_info_changed", async (event) => {
		if (!current) return;
		current.name = event.name;
		await writePresence(current).catch(() => undefined);
	});

	pi.on("session_shutdown", async () => {
		generation += 1;
		if (pollTimer) clearInterval(pollTimer);
		if (heartbeatTimer) clearInterval(heartbeatTimer);
		pollTimer = undefined;
		heartbeatTimer = undefined;
		const session = current;
		current = undefined;
		if (session) await unlink(presencePath(session.id)).catch(() => undefined);
	});
}
