import { spawn } from "node:child_process";
import { existsSync } from "node:fs";
import { basename } from "node:path";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { StringEnum } from "@earendil-works/pi-ai";
import { Text } from "@earendil-works/pi-tui";
import { Type } from "typebox";

const DEFAULT_PROVIDER = "fireworks";
const DEFAULT_MODEL = "accounts/fireworks/models/deepseek-v4-flash-0731";
const MAX_TASKS = 8;
const MAX_CONCURRENCY = 4;
const MAX_OUTPUT_BYTES = 50 * 1024;
const DEFAULT_TIMEOUT_SECONDS = 600;
const DEFAULT_THINKING: Thinking = "low";
const READ_ONLY_TOOLS = "read,grep,find,ls";
const WORKSPACE_TOOLS = "read,bash,edit,write,grep,find,ls";
const STATE_ENTRY = "subagents-default-model";
const SUBAGENT_PROMPT = `You are a focused subagent handling a bounded, low-effort or mechanical task.
Work independently in the provided working directory and follow all applicable AGENTS.md instructions.
Do not delegate to other agents. Do not broaden the task or make unrelated changes.
For research tasks, inspect the source of truth and return concise findings with exact file paths and line references when useful.
For modification tasks, make only the requested changes, verify them when practical, and summarize files changed and checks run.
Keep the investigation bounded: aim for no more than 8 tool calls. Prioritize a useful final answer over exhaustive exploration; report remaining uncertainty instead of continuing to search.
Keep the final response compact and directly useful to the parent agent.`;

type Access = "read-only" | "workspace";
type Thinking = "off" | "minimal" | "low" | "medium" | "high" | "xhigh" | "max";
type TaskStatus = "queued" | "running" | "completed" | "failed" | "aborted";
type ActivityKind = "note" | "tool" | "result";

interface ActivityItem {
	at: number;
	kind: ActivityKind;
	text: string;
}

interface TaskInput {
	task: string;
	provider?: string;
	model?: string;
	cwd?: string;
	access?: Access;
	thinking?: Thinking;
	timeoutSeconds?: number;
}

interface ModelSelection {
	provider: string;
	model: string;
}

interface Usage {
	input: number;
	output: number;
	cacheRead: number;
	cacheWrite: number;
	totalTokens: number;
	cost: {
		input: number;
		output: number;
		cacheRead: number;
		cacheWrite: number;
		total: number;
	};
}

interface TaskResult {
	index: number;
	task: string;
	provider: string;
	model: string;
	cwd: string;
	access: Access;
	thinking: Thinking;
	status: TaskStatus;
	startedAt?: number;
	finishedAt?: number;
	activity: ActivityItem[];
	output: string;
	stderr: string;
	exitCode?: number;
	stopReason?: string;
	errorMessage?: string;
	usage: Usage;
}

interface SubagentsDetails {
	defaultProvider: string;
	defaultModel: string;
	defaultThinking: Thinking;
	results: TaskResult[];
}

const TaskSchema = Type.Object({
	task: Type.String({ description: "A bounded task to delegate" }),
	provider: Type.Optional(
		Type.String({ description: "Provider for this task, such as fireworks or anthropic. Overrides the invocation default." }),
	),
	model: Type.Optional(
		Type.String({
			description:
				'Model ID for this task. May be a bare ID with provider, or "provider/model-id". Overrides the invocation default.',
		}),
	),
	cwd: Type.Optional(Type.String({ description: "Working directory; defaults to the parent session cwd" })),
	access: Type.Optional(
		StringEnum(["read-only", "workspace"] as const, {
			description:
				'"read-only" enables read/search tools only (default). "workspace" also enables bash/edit/write and should only be used for independent, non-overlapping changes.',
		}),
	),
	thinking: Type.Optional(
		StringEnum(["off", "minimal", "low", "medium", "high", "xhigh", "max"] as const, {
			description: 'Thinking level for this task; defaults to the invocation level or "low".',
		}),
	),
	timeoutSeconds: Type.Optional(
		Type.Integer({ minimum: 30, maximum: 900, description: "Task timeout in seconds; defaults to 600" }),
	),
});

const SubagentsSchema = Type.Object({
	tasks: Type.Array(TaskSchema, {
		minItems: 1,
		maxItems: MAX_TASKS,
		description: `Independent tasks to run in parallel (maximum ${MAX_TASKS})`,
	}),
	provider: Type.Optional(
		Type.String({ description: "Default provider for this invocation. Each task may override it." }),
	),
	model: Type.Optional(
		Type.String({
			description:
				'Default model for this invocation. May be a bare ID with provider, or "provider/model-id". Each task may override it; otherwise uses the user/session default.',
		}),
	),
	thinking: Type.Optional(
		StringEnum(["off", "minimal", "low", "medium", "high", "xhigh", "max"] as const, {
			description: 'Default thinking level for this invocation; defaults to "low". Each task may override it.',
		}),
	),
	concurrency: Type.Optional(
		Type.Integer({ minimum: 1, maximum: MAX_CONCURRENCY, description: "Maximum concurrent subprocesses; defaults to 4" }),
	),
});

function emptyUsage(): Usage {
	return {
		input: 0,
		output: 0,
		cacheRead: 0,
		cacheWrite: 0,
		totalTokens: 0,
		cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 },
	};
}

function addUsage(target: Usage, source: any): void {
	if (!source) return;
	target.input += source.input ?? 0;
	target.output += source.output ?? 0;
	target.cacheRead += source.cacheRead ?? 0;
	target.cacheWrite += source.cacheWrite ?? 0;
	target.totalTokens += source.totalTokens ?? 0;
	target.cost.input += source.cost?.input ?? 0;
	target.cost.output += source.cost?.output ?? 0;
	target.cost.cacheRead += source.cost?.cacheRead ?? 0;
	target.cost.cacheWrite += source.cost?.cacheWrite ?? 0;
	target.cost.total += source.cost?.total ?? 0;
}

function aggregateUsage(results: TaskResult[]): Usage {
	const total = emptyUsage();
	for (const result of results) addUsage(total, result.usage);
	return total;
}

function truncateUtf8(value: string, maxBytes = MAX_OUTPUT_BYTES): string {
	const buffer = Buffer.from(value, "utf8");
	if (buffer.byteLength <= maxBytes) return value;
	const kept = buffer.subarray(0, maxBytes).toString("utf8").replace(/\uFFFD$/, "");
	return `${kept}\n\n[Output truncated: ${buffer.byteLength - Buffer.byteLength(kept, "utf8")} bytes omitted.]`;
}

function getAssistantText(message: any): string {
	if (message?.role !== "assistant" || !Array.isArray(message.content)) return "";
	return message.content
		.filter((part: any) => part?.type === "text" && typeof part.text === "string")
		.map((part: any) => part.text)
		.join("\n");
}

function formatElapsed(startedAt?: number, finishedAt?: number): string {
	if (!startedAt) return "0s";
	const seconds = Math.max(0, Math.floor(((finishedAt ?? Date.now()) - startedAt) / 1000));
	if (seconds < 60) return `${seconds}s`;
	return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
}

function summarizeToolCall(toolName: string, args: Record<string, unknown>): string {
	const shorten = (value: unknown, limit = 120) => {
		const text = typeof value === "string" ? value.replace(/\s+/g, " ").trim() : String(value ?? "");
		return text.length > limit ? `${text.slice(0, limit - 1)}…` : text;
	};
	const filePath = args.path ?? args.file_path;
	switch (toolName) {
		case "read":
			return `read ${shorten(filePath ?? "…")}`;
		case "grep":
			return `grep /${shorten(args.pattern ?? "", 70)}/ in ${shorten(filePath ?? ".", 70)}`;
		case "find":
			return `find ${shorten(args.pattern ?? "*", 70)} in ${shorten(filePath ?? ".", 70)}`;
		case "ls":
			return `ls ${shorten(filePath ?? ".")}`;
		case "bash":
			return `$ ${shorten(args.command ?? "…")}`;
		case "edit":
			return `edit ${shorten(filePath ?? "…")}`;
		case "write":
			return `write ${shorten(filePath ?? "…")}`;
		default:
			return `${toolName} ${shorten(JSON.stringify(args), 100)}`;
	}
}

function addActivity(result: TaskResult, kind: ActivityKind, text: string): void {
	const compact = text.replace(/\n{3,}/g, "\n\n").trim();
	if (!compact) return;
	result.activity.push({ at: Date.now(), kind, text: truncateUtf8(compact, 4 * 1024) });
	if (result.activity.length > 60) result.activity.splice(0, result.activity.length - 60);
}

function getPiInvocation(args: string[]): { command: string; args: string[] } {
	const currentScript = process.argv[1];
	const isBunVirtualScript = currentScript?.startsWith("/$bunfs/root/");
	if (currentScript && !isBunVirtualScript && existsSync(currentScript)) {
		return { command: process.execPath, args: [currentScript, ...args] };
	}

	const executable = basename(process.execPath).toLowerCase();
	if (!/^(node|bun)(\.exe)?$/.test(executable)) return { command: process.execPath, args };
	return { command: "pi", args };
}

async function runTask(
	input: TaskInput,
	index: number,
	selection: ModelSelection,
	defaultCwd: string,
	signal: AbortSignal | undefined,
	onChange: (result: TaskResult) => void,
): Promise<TaskResult> {
	const access = input.access ?? "read-only";
	const thinking = input.thinking ?? DEFAULT_THINKING;
	const cwd = input.cwd ?? defaultCwd;
	const result: TaskResult = {
		index,
		task: input.task,
		provider: selection.provider,
		model: selection.model,
		cwd,
		access,
		thinking,
		status: "running",
		startedAt: Date.now(),
		activity: [],
		output: "",
		stderr: "",
		usage: emptyUsage(),
	};
	onChange(result);

	const args = [
		"--mode",
		"json",
		"--print",
		"--no-session",
		"--provider",
		selection.provider,
		"--model",
		selection.model,
		"--thinking",
		thinking,
		"--tools",
		access === "workspace" ? WORKSPACE_TOOLS : READ_ONLY_TOOLS,
		"--append-system-prompt",
		SUBAGENT_PROMPT,
		`Task: ${input.task}`,
	];
	const invocation = getPiInvocation(args);
	const timeoutMs = (input.timeoutSeconds ?? DEFAULT_TIMEOUT_SECONDS) * 1000;

	await new Promise<void>((resolve) => {
		let settled = false;
		let stdoutBuffer = "";
		let finalAssistantText = "";
		const child = spawn(invocation.command, invocation.args, {
			cwd,
			shell: false,
			stdio: ["ignore", "pipe", "pipe"],
		});

		const finish = (status: TaskStatus, exitCode?: number, error?: string) => {
			if (settled) return;
			settled = true;
			clearTimeout(timeout);
			signal?.removeEventListener("abort", abort);
			result.exitCode = exitCode;
			result.status = status;
			result.finishedAt = Date.now();
			if (error) result.errorMessage = error;
			result.output = truncateUtf8(finalAssistantText || result.errorMessage || result.stderr.trim() || "(no output)");
			onChange(result);
			resolve();
		};

		const stop = (status: "failed" | "aborted", reason: string) => {
			if (child.exitCode === null) {
				child.kill("SIGTERM");
				setTimeout(() => {
					if (child.exitCode === null) child.kill("SIGKILL");
				}, 2_000).unref();
			}
			finish(status, child.exitCode ?? undefined, reason);
		};
		const abort = () => stop("aborted", "Subagent task aborted");
		const timeout = setTimeout(() => stop("failed", `Subagent timed out after ${timeoutMs / 1000}s`), timeoutMs);

		const processLine = (line: string) => {
			if (!line.trim()) return;
			let event: any;
			try {
				event = JSON.parse(line);
			} catch {
				return;
			}
			if (event.type === "tool_execution_start") {
				addActivity(result, "tool", summarizeToolCall(event.toolName ?? "tool", event.args ?? {}));
				onChange(result);
				return;
			}
			if (event.type === "tool_execution_end" && event.isError) {
				addActivity(result, "result", `${event.toolName ?? "tool"} failed`);
				onChange(result);
				return;
			}
			if (event.type !== "message_end" || !event.message) return;
			const text = getAssistantText(event.message);
			if (text) {
				finalAssistantText = text;
				addActivity(result, "note", text);
			}
			if (event.message.role === "assistant") {
				addUsage(result.usage, event.message.usage);
				if (event.message.stopReason) result.stopReason = event.message.stopReason;
				if (event.message.errorMessage) result.errorMessage = event.message.errorMessage;
			}
			onChange(result);
		};

		child.stdout.on("data", (chunk) => {
			stdoutBuffer += chunk.toString();
			const lines = stdoutBuffer.split("\n");
			stdoutBuffer = lines.pop() ?? "";
			for (const line of lines) processLine(line);
		});
		child.stderr.on("data", (chunk) => {
			result.stderr = truncateUtf8(result.stderr + chunk.toString());
			onChange(result);
		});
		child.on("error", (error) => finish("failed", undefined, error.message));
		child.on("close", (code) => {
			if (stdoutBuffer.trim()) processLine(stdoutBuffer);
			const failed = code !== 0 || result.stopReason === "error" || result.stopReason === "aborted";
			finish(failed ? (result.stopReason === "aborted" ? "aborted" : "failed") : "completed", code ?? 0);
		});

		if (signal?.aborted) abort();
		else signal?.addEventListener("abort", abort, { once: true });
	});

	return result;
}

async function mapWithConcurrency<T, R>(
	items: T[],
	concurrency: number,
	worker: (item: T, index: number) => Promise<R>,
): Promise<R[]> {
	const results = new Array<R>(items.length);
	let nextIndex = 0;
	const workers = Array.from({ length: Math.min(concurrency, items.length) }, async () => {
		while (true) {
			const index = nextIndex++;
			if (index >= items.length) return;
			results[index] = await worker(items[index]!, index);
		}
	});
	await Promise.all(workers);
	return results;
}

export default function subagentsExtension(pi: ExtensionAPI) {
	let sessionDefault: ModelSelection = { provider: DEFAULT_PROVIDER, model: DEFAULT_MODEL };

	function resolveModel(ctx: any, value: string, providerHint?: string): ModelSelection {
		const raw = value.trim();
		if (!raw) throw new Error("Model ID cannot be empty.");

		if (providerHint) {
			const provider = providerHint.trim();
			if (!provider) throw new Error("Provider cannot be empty.");
			const prefix = `${provider}/`;
			const model = raw.startsWith(prefix) ? raw.slice(prefix.length) : raw;
			if (!ctx.modelRegistry.find(provider, model)) {
				throw new Error(`Unknown or unavailable model: ${provider}/${model}. Use /subagents-model list ${provider} to see available IDs.`);
			}
			return { provider, model };
		}

		if (ctx.modelRegistry.find(sessionDefault.provider, raw)) {
			return { provider: sessionDefault.provider, model: raw };
		}
		const slash = raw.indexOf("/");
		if (slash > 0) {
			const provider = raw.slice(0, slash);
			const model = raw.slice(slash + 1);
			if (ctx.modelRegistry.find(provider, model)) return { provider, model };
		}
		const matches = ctx.modelRegistry.getAvailable().filter((model: any) => model.id === raw);
		if (matches.length === 1) return { provider: matches[0].provider, model: matches[0].id };
		if (matches.length > 1) throw new Error(`Model ID ${raw} exists on multiple providers; use provider/model-id.`);
		throw new Error(`Unknown or unavailable model: ${raw}. Use /subagents-model list [provider] to see available IDs.`);
	}

	function assertProviderAuth(ctx: any, provider: string): void {
		const auth = ctx.modelRegistry.getProviderAuthStatus(provider);
		if (!auth.configured) throw new Error(`${provider} authentication is not configured. Run /login ${provider} first.`);
	}

	pi.on("session_start", (_event, ctx) => {
		sessionDefault = { provider: DEFAULT_PROVIDER, model: DEFAULT_MODEL };
		for (const entry of ctx.sessionManager.getBranch()) {
			if (entry.type !== "custom" || entry.customType !== STATE_ENTRY) continue;
			const data = entry.data as { provider?: unknown; model?: unknown } | undefined;
			if (
				typeof data?.provider === "string" &&
				typeof data.model === "string" &&
				ctx.modelRegistry.find(data.provider, data.model)
			) {
				sessionDefault = { provider: data.provider, model: data.model };
			}
		}
	});

	pi.registerCommand("subagents-model", {
		description: "Show or set the session default: /subagents-model [list [provider]|reset|provider/model-id]",
		async handler(args, ctx) {
			const value = args.trim();
			if (!value) {
				ctx.ui.notify(`Subagents default: ${sessionDefault.provider}/${sessionDefault.model}`, "info");
				return;
			}
			const listMatch = value.match(/^list(?:\s+(\S+))?$/);
			if (listMatch) {
				const provider = listMatch[1];
				const models = ctx.modelRegistry
					.getAvailable()
					.filter((model) => !provider || model.provider === provider)
					.map((model) => `${model.provider}/${model.id}`)
					.sort();
				ctx.ui.notify(`Available models${provider ? ` for ${provider}` : ""} (${models.length}):\n${models.join("\n")}`, "info");
				return;
			}
			try {
				sessionDefault = value === "reset" ? { provider: DEFAULT_PROVIDER, model: DEFAULT_MODEL } : resolveModel(ctx, value);
				assertProviderAuth(ctx, sessionDefault.provider);
				pi.appendEntry(STATE_ENTRY, sessionDefault);
				ctx.ui.notify(`Subagents default set to ${sessionDefault.provider}/${sessionDefault.model}`, "info");
			} catch (error) {
				ctx.ui.notify(error instanceof Error ? error.message : String(error), "error");
			}
		},
	});

	pi.registerTool({
		name: "subagents",
		label: "Subagents",
		description:
			"Run 1-8 independent low-effort, research, or mechanical tasks in isolated Pi subprocesses, in parallel. Accepts invocation-level and per-task provider/model overrides; otherwise uses the user's /subagents-model session default. Tasks are read-only unless workspace access is explicitly requested.",
		promptSnippet: "Delegate independent low-effort, research, or mechanical tasks to parallel subagents",
		promptGuidelines: [
			"Use subagents for independent reconnaissance, repetitive checks, or bounded mechanical work that benefits from parallel execution.",
			"Do not use subagents for tiny tasks that are faster to do directly, tightly coupled tasks, or decisions requiring the full conversation context.",
			"When using subagents with workspace access in parallel, assign non-overlapping files or directories to avoid conflicting writes.",
		],
		parameters: SubagentsSchema,
		async execute(_toolCallId, params, signal, onUpdate, ctx) {
			if (params.provider && !params.model) throw new Error("An invocation-level provider override also requires model.");
			const invocation = params.model ? resolveModel(ctx, params.model, params.provider) : sessionDefault;
			const resolved = params.tasks.map((task, index) => {
				if (task.provider && !task.model) throw new Error(`Task ${index + 1}: a provider override also requires model.`);
				let selection = invocation;
				if (task.model && task.provider) selection = resolveModel(ctx, task.model, task.provider);
				else if (task.model && ctx.modelRegistry.find(invocation.provider, task.model.trim())) {
					selection = { provider: invocation.provider, model: task.model.trim() };
				} else if (task.model) selection = resolveModel(ctx, task.model);
				return { ...task, index, selection, thinking: task.thinking ?? params.thinking ?? DEFAULT_THINKING };
			});
			for (const provider of new Set(resolved.map((task) => task.selection.provider))) assertProviderAuth(ctx, provider);

			const results: TaskResult[] = resolved.map((task) => ({
				index: task.index,
				task: task.task,
				provider: task.selection.provider,
				model: task.selection.model,
				cwd: task.cwd ?? ctx.cwd,
				access: task.access ?? "read-only",
				thinking: task.thinking,
				status: "queued",
				activity: [],
				output: "",
				stderr: "",
				usage: emptyUsage(),
			}));
			const details = (): SubagentsDetails => ({
				defaultProvider: invocation.provider,
				defaultModel: invocation.model,
				defaultThinking: params.thinking ?? DEFAULT_THINKING,
				results: results.map((result) => ({ ...result, activity: [...result.activity] })),
			});

			const emitUpdate = () => {
				const done = results.filter((result) => ["completed", "failed", "aborted"].includes(result.status)).length;
				onUpdate?.({
					content: [{ type: "text", text: `Subagents: ${done}/${results.length} finished` }],
					details: details(),
				});
			};

			await mapWithConcurrency(resolved, params.concurrency ?? MAX_CONCURRENCY, async (task, index) => {
				const result = await runTask(task, index, task.selection, ctx.cwd, signal, (update) => {
					results[index] = { ...update };
					emitUpdate();
				});
				results[index] = result;
				return result;
			});

			const succeeded = results.filter((result) => result.status === "completed").length;
			const sections = results.map((result) => {
				const heading = `### Task ${result.index + 1} — ${result.status} — ${result.provider}/${result.model}`;
				const diagnostic = result.errorMessage ? `Error: ${result.errorMessage}\n\n` : "";
				return `${heading}\n\n${diagnostic}${result.output}`;
			});
			return {
				content: [
					{
						type: "text",
						text: truncateUtf8(`Subagents: ${succeeded}/${results.length} succeeded\n\n${sections.join("\n\n---\n\n")}`),
					},
				],
				details: details(),
				usage: aggregateUsage(results),
			};
		},
		renderCall(args, theme) {
			const tasks = args.tasks ?? [];
			const model = args.model ? `${args.provider ? `${args.provider}/` : ""}${args.model}` : "session default";
			let text = `${theme.fg("toolTitle", theme.bold("subagents "))}${theme.fg("accent", `${tasks.length} task${tasks.length === 1 ? "" : "s"}`)}${theme.fg("muted", ` · ${model}`)}`;
			for (const task of tasks.slice(0, 4)) {
				const preview = task.task.length > 70 ? `${task.task.slice(0, 67)}…` : task.task;
				text += `\n  ${theme.fg("dim", preview)}`;
			}
			if (tasks.length > 4) text += `\n  ${theme.fg("muted", `… ${tasks.length - 4} more`)}`;
			return new Text(text, 0, 0);
		},
		renderResult(result, { expanded }, theme) {
			const details = result.details as SubagentsDetails | undefined;
			if (!details) {
				const content = result.content[0];
				return new Text(content?.type === "text" ? content.text : "(no output)", 0, 0);
			}
			const complete = details.results.filter((item) => item.status === "completed").length;
			const finished = details.results.filter((item) => ["completed", "failed", "aborted"].includes(item.status)).length;
			const running = finished < details.results.length;
			const icon = running ? theme.fg("warning", "⏳") : complete === details.results.length ? theme.fg("success", "✓") : theme.fg("warning", "◐");
			let text = `${icon} ${theme.fg("toolTitle", theme.bold("subagents "))}${theme.fg("accent", `${finished}/${details.results.length} finished`)}`;
			for (const item of details.results) {
				const itemIcon = item.status === "completed" ? theme.fg("success", "✓") : item.status === "failed" || item.status === "aborted" ? theme.fg("error", "✗") : theme.fg("warning", "⏳");
				text += `\n${itemIcon} ${theme.fg("accent", `Task ${item.index + 1}`)}${theme.fg("muted", ` · ${formatElapsed(item.startedAt, item.finishedAt)} · ${item.provider}/${item.model} · ${item.thinking} · ${item.access}`)}`;
				if (item.errorMessage) text += `\n  ${theme.fg("error", item.errorMessage)}`;
				const latestActivity = item.activity.at(-1);
				if (!expanded && running && latestActivity) {
					text += `\n  ${theme.fg("dim", `→ ${latestActivity.text.split("\n")[0]}`)}`;
				}
				if (expanded && running) {
					for (const activity of item.activity.slice(-15)) {
						const marker = activity.kind === "tool" ? "→" : activity.kind === "result" ? "✓" : "·";
						text += `\n  ${theme.fg(activity.kind === "tool" ? "warning" : activity.kind === "result" ? "success" : "toolOutput", `${marker} ${activity.text.split("\n")[0]}`)}`;
					}
				}
				if (expanded && !running && item.output) text += `\n${theme.fg("toolOutput", item.output)}`;
			}
			if (!expanded && !running) text += `\n${theme.fg("muted", "(Ctrl+O to expand outputs)")}`;
			return new Text(text, 0, 0);
		},
	});
}
