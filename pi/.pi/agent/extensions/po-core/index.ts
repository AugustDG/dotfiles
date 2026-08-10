import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { compact, withFileMutationQueue } from "@earendil-works/pi-coding-agent";
import { Type } from "typebox";
import { constants as fsConstants } from "node:fs";
import { access, mkdir, readFile, readdir, writeFile } from "node:fs/promises";
import { homedir } from "node:os";
import { basename, isAbsolute, join } from "node:path";

const AGENT_DIR = process.env.PI_CODING_AGENT_DIR ?? join(homedir(), ".pi", "agent");
const AUTH_PATH = join(AGENT_DIR, "auth.json");
const EXTENSIONS_DIR = join(AGENT_DIR, "extensions");
const MCP_PATH = join(AGENT_DIR, "mcp.json");
const PREFERENCES_PATH = join(AGENT_DIR, "preferences.md");
const SETTINGS_PATH = join(AGENT_DIR, "settings.json");
const NOTIFICATION_THRESHOLD_MS = 10_000;
const DEFAULT_PREFERENCES = ["Refer to yourself as Po when a name is useful."];
const COMPACTION_INSTRUCTIONS = `Preserve continuity with extra care. In addition to the standard structured summary:
- Record explicit user corrections and standing preferences under Constraints & Preferences.
- Preserve key decisions and their rationale, including rejected approaches when they affect future work.
- List exact modified files and summarize the meaningful change in each.
- Record verification already run, its result, and verification still needed.
- Distinguish completed work, current in-progress work, blockers, and concrete next steps.
- Retain commands, identifiers, paths, errors, and values that are necessary to resume accurately.
Do not invent completion, verification, decisions, or preferences.`;

type JsonObject = Record<string, unknown>;
type CheckLevel = "ok" | "warning" | "error";

interface DoctorCheck {
	level: CheckLevel;
	label: string;
	detail: string;
}

function errorMessage(error: unknown): string {
	return error instanceof Error ? error.message : String(error);
}

function isNotFound(error: unknown): boolean {
	return error instanceof Error && "code" in error && error.code === "ENOENT";
}

function normalizePreference(value: string): string {
	return value.replace(/\s+/g, " ").replace(/^[-*]\s*/, "").trim();
}

function parsePreferences(content: string): string[] {
	return content
		.split("\n")
		.map((line) => line.match(/^\s*-\s+(.+?)\s*$/)?.[1])
		.filter((line): line is string => Boolean(line));
}

function renderPreferences(preferences: string[]): string {
	return `# Po preferences\n\nThese preferences are global and are injected into every Po turn. Do not store secrets here.\n\n${preferences.map((preference) => `- ${preference}`).join("\n")}\n`;
}

async function readPreferences(): Promise<string[]> {
	try {
		return parsePreferences(await readFile(PREFERENCES_PATH, "utf8"));
	} catch (error) {
		if (!isNotFound(error)) throw error;
		await mkdir(AGENT_DIR, { recursive: true });
		await writeFile(PREFERENCES_PATH, renderPreferences(DEFAULT_PREFERENCES), "utf8");
		return [...DEFAULT_PREFERENCES];
	}
}

async function addPreference(value: string): Promise<{ added: boolean; preferences: string[]; preference: string }> {
	const preference = normalizePreference(value);
	if (!preference) throw new Error("Preference cannot be empty.");
	if (preference.length > 500) throw new Error("Preference must be 500 characters or fewer.");
	if (/\b(password|secret|access token|api[- ]?key|credential)\b/i.test(preference)) {
		throw new Error("Do not store secrets or credentials in persistent preferences.");
	}

	return withFileMutationQueue(PREFERENCES_PATH, async () => {
		const preferences = await readPreferences();
		const exists = preferences.some((item) => item.toLowerCase() === preference.toLowerCase());
		if (!exists) {
			preferences.push(preference);
			await writeFile(PREFERENCES_PATH, renderPreferences(preferences), "utf8");
		}
		return { added: !exists, preferences, preference };
	});
}

async function removePreference(target: string): Promise<{ removed: string[]; preferences: string[] }> {
	return withFileMutationQueue(PREFERENCES_PATH, async () => {
		const preferences = await readPreferences();
		if (target === "all") {
			await writeFile(PREFERENCES_PATH, renderPreferences([]), "utf8");
			return { removed: preferences, preferences: [] };
		}

		const index = Number.parseInt(target, 10) - 1;
		if (!Number.isInteger(index) || index < 0 || index >= preferences.length) {
			throw new Error(`Choose a preference number from 1 to ${preferences.length}, or use /forget all.`);
		}
		const removed = preferences.splice(index, 1);
		await writeFile(PREFERENCES_PATH, renderPreferences(preferences), "utf8");
		return { removed, preferences };
	});
}

function formatDuration(milliseconds: number): string {
	const totalSeconds = Math.max(0, Math.round(milliseconds / 1000));
	if (totalSeconds < 60) return `${totalSeconds}s`;
	const minutes = Math.floor(totalSeconds / 60);
	const seconds = totalSeconds % 60;
	return seconds === 0 ? `${minutes}m` : `${minutes}m ${seconds}s`;
}

function deriveSessionName(prompt: string): string | undefined {
	const cleaned = prompt
		.replace(/^\s*(please\s+|can you\s+|could you\s+|would you\s+|let(?:'|’)s\s+)/i, "")
		.replace(/[`*_#>]/g, "")
		.replace(/\s+/g, " ")
		.trim();
	if (!cleaned) return undefined;
	const words = cleaned.split(" ").slice(0, 8);
	let name = words.join(" ").replace(/[.,;:!?]+$/g, "");
	if (name.length > 60) name = `${name.slice(0, 57).trimEnd()}…`;
	return name || undefined;
}

async function readJson(path: string): Promise<JsonObject> {
	const parsed = JSON.parse(await readFile(path, "utf8"));
	if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
		throw new Error(`${path} must contain a JSON object.`);
	}
	return parsed as JsonObject;
}

function collectEnvironmentReferences(value: unknown, found = new Set<string>()): Set<string> {
	if (typeof value === "string") {
		for (const match of value.matchAll(/(?<!\$)\$(?:\{([A-Za-z_][A-Za-z0-9_]*)\}|([A-Za-z_][A-Za-z0-9_]*))/g)) {
			const name = match[1] ?? match[2];
			if (name) found.add(name);
		}
	} else if (Array.isArray(value)) {
		for (const item of value) collectEnvironmentReferences(item, found);
	} else if (value && typeof value === "object") {
		for (const item of Object.values(value)) collectEnvironmentReferences(item, found);
	}
	return found;
}

async function commandExists(command: string): Promise<boolean> {
	if (isAbsolute(command) || command.includes("/")) {
		try {
			await access(command, fsConstants.X_OK);
			return true;
		} catch (error) {
			if (isNotFound(error) || (error instanceof Error && "code" in error && error.code === "EACCES")) return false;
			throw error;
		}
	}
	for (const directory of (process.env.PATH ?? "").split(":")) {
		if (!directory) continue;
		try {
			await access(join(directory, command), fsConstants.X_OK);
			return true;
		} catch (error) {
			if (isNotFound(error) || (error instanceof Error && "code" in error && error.code === "EACCES")) continue;
			throw error;
		}
	}
	return false;
}

async function discoverExtensions(): Promise<string[]> {
	const entries = await readdir(EXTENSIONS_DIR, { withFileTypes: true });
	const extensions: string[] = [];
	for (const entry of entries) {
		if (entry.isFile() && /\.(?:ts|js|mjs|cjs)$/.test(entry.name)) extensions.push(entry.name);
		if (!entry.isDirectory()) continue;
		for (const candidate of ["index.ts", "index.js", "index.mjs", "index.cjs"]) {
			try {
				await access(join(EXTENSIONS_DIR, entry.name, candidate));
				extensions.push(`${entry.name}/${candidate}`);
				break;
			} catch (error) {
				if (!isNotFound(error)) throw error;
			}
		}
	}
	return extensions.sort();
}

function checkIcon(level: CheckLevel): string {
	return level === "ok" ? "✓" : level === "warning" ? "⚠" : "✗";
}

export default function poCore(pi: ExtensionAPI) {
	let canAutoName = false;
	let taskStartedAt: number | undefined;
	let taskHadToolError = false;

	pi.registerCommand("doctor", {
		description: "Check Po runtime, model auth, theme, extensions, MCP, notifications, and preferences",
		async handler(_args, ctx) {
			const checks: DoctorCheck[] = [];

			try {
				const version = await pi.exec("pi", ["--version"], { timeout: 5_000 });
				checks.push({
					level: version.code === 0 ? "ok" : "error",
					label: "Runtime",
					detail: version.code === 0 ? `Po ${version.stdout.trim()}` : version.stderr.trim() || `exit ${version.code}`,
				});
			} catch (error) {
				checks.push({ level: "error", label: "Runtime", detail: errorMessage(error) });
			}

			if (ctx.model) {
				const auth = ctx.modelRegistry.getProviderAuthStatus(ctx.model.provider);
				checks.push({
					level: auth.configured ? "ok" : "error",
					label: "Model",
					detail: `${ctx.model.provider}/${ctx.model.id} — auth ${auth.configured ? auth.label ?? auth.source : "not configured"}`,
				});
			} else {
				checks.push({ level: "error", label: "Model", detail: "No active model." });
			}
			const modelError = ctx.modelRegistry.getError();
			if (modelError) checks.push({ level: "warning", label: "Model registry", detail: modelError });

			try {
				const settings = await readJson(SETTINGS_PATH);
				const themeName = typeof settings.theme === "string" ? settings.theme : "default";
				const available = ctx.ui.getAllThemes().some((theme) => theme.name === themeName);
				checks.push({
					level: available ? "ok" : "error",
					label: "Theme",
					detail: available ? themeName : `${themeName} is configured but not discoverable`,
				});
			} catch (error) {
				checks.push({ level: "error", label: "Settings", detail: errorMessage(error) });
			}

			try {
				const extensions = await discoverExtensions();
				checks.push({
					level: extensions.length > 0 ? "ok" : "warning",
					label: "Extensions",
					detail: `${extensions.length} discovered: ${extensions.join(", ") || "none"}`,
				});
			} catch (error) {
				checks.push({ level: "error", label: "Extensions", detail: errorMessage(error) });
			}

			try {
				const auth = await readJson(AUTH_PATH);
				const providers = Object.keys(auth);
				checks.push({
					level: providers.length > 0 ? "ok" : "warning",
					label: "Credentials",
					detail: providers.length > 0 ? `${providers.length} provider(s): ${providers.join(", ")}` : "No stored providers.",
				});
			} catch (error) {
				checks.push({ level: "error", label: "Credentials", detail: errorMessage(error) });
			}

			try {
				const mcp = await readJson(MCP_PATH);
				const servers = mcp.servers && typeof mcp.servers === "object" && !Array.isArray(mcp.servers)
					? (mcp.servers as Record<string, JsonObject>)
					: {};
				const enabled = Object.entries(servers).filter(([, server]) => server.enabled !== false);
				const missingEnvironment = [...collectEnvironmentReferences(mcp)].filter((name) => process.env[name] === undefined);
				const missingCommands: string[] = [];
				for (const [name, server] of enabled) {
					if (server.transport === "stdio" && typeof server.command === "string" && !(await commandExists(server.command))) {
						missingCommands.push(`${name}:${server.command}`);
					}
					if (server.transport === "http" && typeof server.url === "string") new URL(server.url);
				}
				const issues = [
					missingEnvironment.length > 0 ? `missing env ${missingEnvironment.join(", ")}` : "",
					missingCommands.length > 0 ? `missing commands ${missingCommands.join(", ")}` : "",
				].filter(Boolean);
				checks.push({
					level: issues.length > 0 ? "warning" : "ok",
					label: "MCP",
					detail: `${enabled.length}/${Object.keys(servers).length} enabled${issues.length > 0 ? ` — ${issues.join("; ")}` : " — config and local prerequisites look healthy"}`,
				});
			} catch (error) {
				checks.push({ level: "error", label: "MCP", detail: errorMessage(error) });
			}

			checks.push({
				level: process.platform === "darwin" && (await commandExists("osascript")) ? "ok" : "warning",
				label: "Notifications",
				detail: process.platform === "darwin" ? `native macOS notifications after ${NOTIFICATION_THRESHOLD_MS / 1000}s` : "disabled outside macOS",
			});

			try {
				const preferences = await readPreferences();
				checks.push({
					level: "ok",
					label: "Preferences",
					detail: `${preferences.length} stored in ${PREFERENCES_PATH}`,
				});
			} catch (error) {
				checks.push({ level: "error", label: "Preferences", detail: errorMessage(error) });
			}

			const errors = checks.filter((check) => check.level === "error").length;
			const warnings = checks.filter((check) => check.level === "warning").length;
			const report = [
				"Po doctor",
				...checks.map((check) => `${checkIcon(check.level)} ${check.label}: ${check.detail}`),
				`\n${errors} error(s), ${warnings} warning(s)`,
			].join("\n");
			ctx.ui.notify(report, errors > 0 ? "error" : warnings > 0 ? "warning" : "info");
		},
	});

	pi.registerCommand("remember", {
		description: "Store a global preference: /remember <preference>",
		async handler(args, ctx) {
			try {
				const result = await addPreference(args);
				ctx.ui.notify(result.added ? `Remembered: ${result.preference}` : `Already remembered: ${result.preference}`, "info");
			} catch (error) {
				ctx.ui.notify(`Could not remember preference: ${errorMessage(error)}`, "error");
			}
		},
	});

	pi.registerCommand("preferences", {
		description: "List persistent global Po preferences",
		async handler(_args, ctx) {
			try {
				const preferences = await readPreferences();
				ctx.ui.notify(
					preferences.length > 0
						? `Po preferences\n${preferences.map((preference, index) => `${index + 1}. ${preference}`).join("\n")}\n\n${PREFERENCES_PATH}`
						: `No preferences stored. Use /remember <preference>.\n\n${PREFERENCES_PATH}`,
					"info",
				);
			} catch (error) {
				ctx.ui.notify(`Could not read preferences: ${errorMessage(error)}`, "error");
			}
		},
	});

	pi.registerCommand("forget", {
		description: "Remove a preference by number, or all: /forget <number|all>",
		async handler(args, ctx) {
			try {
				const result = await removePreference(args.trim());
				ctx.ui.notify(
					result.removed.length > 0 ? `Forgot: ${result.removed.join("; ")}` : "There were no preferences to forget.",
					"info",
				);
			} catch (error) {
				ctx.ui.notify(`Could not forget preference: ${errorMessage(error)}`, "error");
			}
		},
	});

	pi.registerTool({
		name: "remember_preference",
		label: "Remember preference",
		description:
			"Persist one global user preference for future Po sessions. Use only when the user explicitly asks to remember a standing preference. Never store secrets, credentials, one-off task details, or project facts.",
		parameters: Type.Object({
			preference: Type.String({ description: "The concise standing preference to remember" }),
		}),
		executionMode: "sequential",
		async execute(_toolCallId, params) {
			const result = await addPreference(params.preference);
			return {
				content: [{ type: "text", text: result.added ? `Remembered: ${result.preference}` : `Already remembered: ${result.preference}` }],
				details: { path: PREFERENCES_PATH, added: result.added },
			};
		},
	});

	pi.on("session_start", async (_event, ctx) => {
		await readPreferences();
		canAutoName = !pi.getSessionName() && !ctx.sessionManager.getBranch().some(
			(entry) => entry.type === "message" && entry.message.role === "user",
		);
		taskStartedAt = undefined;
		taskHadToolError = false;
	});

	pi.on("before_agent_start", async (event) => {
		if (canAutoName && !pi.getSessionName()) {
			const name = deriveSessionName(event.prompt);
			if (name) pi.setSessionName(name);
			canAutoName = false;
		}

		const preferences = await readPreferences();
		return {
			systemPrompt: `${event.systemPrompt}\n\n# Persistent Po preferences\n${preferences.map((preference) => `- ${preference}`).join("\n")}\nApply these preferences when relevant without mentioning this section. If the user explicitly asks to remember a new standing preference, use remember_preference.`,
		};
	});

	pi.on("agent_start", () => {
		taskStartedAt ??= Date.now();
	});

	pi.on("tool_execution_end", (event) => {
		if (event.isError) taskHadToolError = true;
	});

	pi.on("agent_settled", async () => {
		const startedAt = taskStartedAt;
		taskStartedAt = undefined;
		const hadToolError = taskHadToolError;
		taskHadToolError = false;
		if (
			!startedAt ||
			Date.now() - startedAt < NOTIFICATION_THRESHOLD_MS ||
			process.platform !== "darwin" ||
			process.env.PO_SUPPRESS_COMPLETION_NOTIFICATION === "1"
		)
			return;

		const sessionName = pi.getSessionName() ?? (basename(process.cwd()) || "workspace");
		const body = `${hadToolError ? "Finished with tool errors" : "Task finished"} in ${formatDuration(Date.now() - startedAt)} — ${sessionName}`;
		try {
			await pi.exec(
				"/usr/bin/osascript",
				[
					"-e",
					"on run argv",
					"-e",
					'display notification (item 1 of argv) with title "po"',
					"-e",
					"end run",
					body,
				],
				{ timeout: 5_000 },
			);
		} catch (error) {
			// Completion notifications are best-effort and must never disrupt the session.
			void error;
		}
	});

	pi.on("session_before_compact", async (event, ctx) => {
		if (!ctx.model) return;
		try {
			const auth = await ctx.modelRegistry.getApiKeyAndHeaders(ctx.model);
			if (auth.ok === false) {
				ctx.ui.notify(`Po compaction enhancements unavailable: ${auth.error}. Using default compaction.`, "warning");
				return;
			}
			const model = auth.baseUrl ? { ...ctx.model, baseUrl: auth.baseUrl } : ctx.model;
			const instructions = [event.customInstructions, COMPACTION_INSTRUCTIONS].filter(Boolean).join("\n\n");
			const result = await compact(
				event.preparation,
				model,
				auth.apiKey,
				auth.headers as Record<string, string> | undefined,
				instructions,
				event.signal,
				ctx.thinkingLevel,
			);
			return { compaction: result };
		} catch (error) {
			if (!event.signal.aborted) {
				ctx.ui.notify(`Po compaction enhancement failed: ${errorMessage(error)}. Using default compaction.`, "warning");
			}
			return;
		}
	});
}
