import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import {
	DEFAULT_MAX_BYTES,
	DEFAULT_MAX_LINES,
	truncateHead,
} from "@earendil-works/pi-coding-agent";
import { StringEnum } from "@earendil-works/pi-ai";
import { Type, type TSchema } from "typebox";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import {
	getDefaultEnvironment,
	StdioClientTransport,
} from "@modelcontextprotocol/sdk/client/stdio.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import type { Transport } from "@modelcontextprotocol/sdk/shared/transport.js";
import { z } from "zod";
import { createHash } from "node:crypto";
import { chmod, mkdtemp, readFile, writeFile } from "node:fs/promises";
import { homedir, tmpdir } from "node:os";
import { dirname, isAbsolute, join, resolve } from "node:path";

interface BridgeDefaults {
	startupTimeoutMs?: number;
	toolTimeoutMs?: number;
}

interface ServerBase {
	enabled?: boolean;
	description?: string;
	startupTimeoutMs?: number;
	toolTimeoutMs?: number;
}

interface HttpServerConfig extends ServerBase {
	transport: "http";
	url: string;
	headers?: Record<string, string>;
}

interface StdioServerConfig extends ServerBase {
	transport: "stdio";
	command: string;
	args?: string[];
	cwd?: string;
	env?: Record<string, string>;
}

type ServerConfig = HttpServerConfig | StdioServerConfig;

interface BridgeConfig {
	servers: Record<string, ServerConfig>;
	defaults?: BridgeDefaults;
}

interface McpTool {
	name: string;
	description?: string;
	inputSchema: TSchema;
	annotations?: {
		readOnlyHint?: boolean;
		destructiveHint?: boolean;
		idempotentHint?: boolean;
		openWorldHint?: boolean;
	};
}

interface Connection {
	client: Client;
	transport: Transport;
	tools: McpTool[];
	cwd: string;
}

interface LoaderMatch {
	server: string;
	tool: McpTool;
	toolName: string;
	score: number;
}

const CONFIG_PATH = join(
	process.env.PI_CODING_AGENT_DIR ?? join(homedir(), ".pi", "agent"),
	"mcp.json",
);
const DEFAULT_STARTUP_TIMEOUT_MS = 15_000;
const DEFAULT_TOOL_TIMEOUT_MS = 120_000;
const MAX_TOOL_NAME_LENGTH = 64;
const LenientListToolsResultSchema = z
	.object({
		tools: z.array(
			z
				.object({
					name: z.string(),
					description: z.string().optional(),
					inputSchema: z.record(z.string(), z.unknown()),
					annotations: z.record(z.string(), z.unknown()).optional(),
				})
				.passthrough(),
		),
		nextCursor: z.string().optional(),
	})
	.passthrough();

function expandEnvironment(value: string): string {
	const placeholder = "\u0000PI_MCP_DOLLAR\u0000";
	const escaped = value.replaceAll("$$", placeholder);
	const expanded = escaped.replace(
		/\$(?:\{([A-Za-z_][A-Za-z0-9_]*)\}|([A-Za-z_][A-Za-z0-9_]*))/g,
		(_match, braced: string | undefined, plain: string | undefined) => {
			const name = braced ?? plain;
			const resolved = name ? process.env[name] : undefined;
			if (resolved === undefined) {
				throw new Error(`Required environment variable ${name} is not set`);
			}
			return resolved;
		},
	);
	return expanded.replaceAll(placeholder, "$");
}

function expandPath(value: string, cwd: string): string {
	const expanded = expandEnvironment(value);
	if (expanded === "~") return homedir();
	if (expanded.startsWith("~/")) return join(homedir(), expanded.slice(2));
	return isAbsolute(expanded) ? expanded : resolve(cwd, expanded);
}

function resolveStringMap(values: Record<string, string> | undefined): Record<string, string> {
	return Object.fromEntries(
		Object.entries(values ?? {}).map(([key, value]) => [key, expandEnvironment(value)]),
	);
}

function redactError(error: unknown, config: BridgeConfig): string {
	let message = error instanceof Error ? error.message : String(error);
	for (const server of Object.values(config.servers)) {
		if (server.transport !== "http") continue;
		for (const value of Object.values(server.headers ?? {})) {
			try {
				const resolved = expandEnvironment(value);
				if (resolved.length >= 8) message = message.replaceAll(resolved, "<redacted>");
			} catch {
				// An unresolved variable contains no secret value to redact.
			}
		}
	}
	return message;
}

async function readConfig(): Promise<BridgeConfig> {
	const parsed = JSON.parse(await readFile(CONFIG_PATH, "utf8")) as Partial<BridgeConfig>;
	if (!parsed.servers || typeof parsed.servers !== "object") {
		throw new Error(`${CONFIG_PATH} must define a servers object`);
	}
	return { servers: parsed.servers, defaults: parsed.defaults ?? {} };
}

function slug(value: string): string {
	const normalized = value.toLowerCase().replace(/[^a-z0-9_]+/g, "_").replace(/^_+|_+$/g, "");
	return normalized || "tool";
}

function shortHash(value: string): string {
	return createHash("sha256").update(value).digest("hex").slice(0, 8);
}

function remoteToolName(server: string, tool: string, usedNames: Set<string>): string {
	const base = `mcp_${slug(server)}_${slug(tool)}`;
	let candidate = base;
	if (candidate.length > MAX_TOOL_NAME_LENGTH || usedNames.has(candidate)) {
		const suffix = `_${shortHash(`${server}/${tool}`)}`;
		candidate = `${base.slice(0, MAX_TOOL_NAME_LENGTH - suffix.length)}${suffix}`;
	}
	return candidate;
}

function scoreTool(tool: McpTool, query: string): number {
	const terms = query.toLowerCase().split(/[^a-z0-9]+/).filter(Boolean);
	if (terms.length === 0) return 1;
	const name = tool.name.toLowerCase();
	const haystack = `${tool.name} ${tool.description ?? ""}`.toLowerCase();
	return terms.reduce((score, term) => {
		if (name === term) return score + 8;
		if (name.includes(term)) return score + 4;
		return haystack.includes(term) ? score + 1 : score;
	}, 0);
}

async function formatTextOutput(text: string): Promise<string> {
	const truncated = truncateHead(text, {
		maxBytes: DEFAULT_MAX_BYTES,
		maxLines: DEFAULT_MAX_LINES,
	});
	if (!truncated.truncated) return truncated.content;

	const directory = await mkdtemp(join(tmpdir(), "pi-mcp-"));
	const outputPath = join(directory, "result.txt");
	await writeFile(outputPath, text, "utf8");
	await chmod(outputPath, 0o600);
	return `${truncated.content}\n\n[Output truncated. Full MCP result saved to ${outputPath}]`;
}

async function normalizeToolResult(result: Record<string, unknown>): Promise<{
	content: Array<{ type: "text"; text: string } | { type: "image"; data: string; mimeType: string }>;
	isError: boolean;
}> {
	if (!("content" in result) || !Array.isArray(result.content)) {
		return {
			content: [{ type: "text", text: await formatTextOutput(JSON.stringify(result.toolResult ?? result, null, 2)) }],
			isError: false,
		};
	}

	const textParts: string[] = [];
	const images: Array<{ type: "image"; data: string; mimeType: string }> = [];
	for (const item of result.content as Array<Record<string, unknown>>) {
		switch (item.type) {
			case "text":
				textParts.push(String(item.text ?? ""));
				break;
			case "image":
				if (typeof item.data === "string" && typeof item.mimeType === "string") {
					images.push({ type: "image", data: item.data, mimeType: item.mimeType });
				}
				break;
			case "resource": {
				const resource = item.resource as Record<string, unknown> | undefined;
				if (typeof resource?.text === "string") {
					textParts.push(`${String(resource.uri ?? "resource")}\n${resource.text}`);
				} else {
					textParts.push(`[Binary resource: ${String(resource?.uri ?? "unknown")}]`);
				}
				break;
			}
			case "resource_link":
				textParts.push(`[Resource: ${String(item.name ?? "link")}] ${String(item.uri ?? "")}`);
				break;
			case "audio":
				textParts.push(`[Audio result: ${String(item.mimeType ?? "unknown type")}]`);
				break;
			default:
				textParts.push(JSON.stringify(item));
		}
	}

	if (textParts.length === 0 && result.structuredContent !== undefined) {
		textParts.push(JSON.stringify(result.structuredContent, null, 2));
	}
	const content: Array<{ type: "text"; text: string } | { type: "image"; data: string; mimeType: string }> = [];
	if (textParts.length > 0) {
		content.push({ type: "text", text: await formatTextOutput(textParts.join("\n\n")) });
	}
	content.push(...images);
	if (content.length === 0) content.push({ type: "text", text: "MCP tool completed without output." });
	return { content, isError: result.isError === true };
}

export default function mcpExtension(pi: ExtensionAPI) {
	let config: BridgeConfig = { servers: {} };
	let configError: string | undefined;
	const connections = new Map<string, Connection>();
	const connecting = new Map<string, Promise<Connection>>();
	const registered = new Map<string, Map<string, string>>();
	const usedToolNames = new Set<string>(["mcp"]);
	let lastContext: ExtensionContext | undefined;

	const loadConfig = async () => {
		try {
			config = await readConfig();
			configError = undefined;
		} catch (error) {
			config = { servers: {} };
			configError = error instanceof Error ? error.message : String(error);
		}
	};

	const updateStatus = (ctx: ExtensionContext) => {
		lastContext = ctx;
		const connected = connections.size;
		ctx.ui.setStatus(
			"mcp",
			connected > 0 ? ctx.ui.theme.fg("dim", `MCP ${connected}/${Object.keys(config.servers).length}`) : undefined,
		);
	};

	const closeServer = async (name: string) => {
		const connection = connections.get(name);
		connections.delete(name);
		connecting.delete(name);
		if (connection) await connection.client.close();
	};

	const closeAll = async () => {
		const names = [...connections.keys()];
		await Promise.allSettled(names.map(closeServer));
		if (lastContext) updateStatus(lastContext);
	};

	const createConnection = async (
		name: string,
		server: ServerConfig,
		cwd: string,
		signal?: AbortSignal,
	): Promise<Connection> => {
		const startupTimeout =
			server.startupTimeoutMs ?? config.defaults?.startupTimeoutMs ?? DEFAULT_STARTUP_TIMEOUT_MS;
		const client = new Client({ name: "pi-mcp-bridge", version: "1.0.0" }, { capabilities: {} });
		let transport: Transport;

		if (server.transport === "http") {
			transport = new StreamableHTTPClientTransport(new URL(expandEnvironment(server.url)), {
				requestInit: {
					headers: {
						...resolveStringMap(server.headers),
						// Undici can advertise zstd even when the active runtime cannot decode it,
						// which throws from its response parser as an uncaught exception.
						"accept-encoding": "br, gzip, deflate",
					},
				},
			});
		} else {
			const inherited = getDefaultEnvironment();
			transport = new StdioClientTransport({
				command: expandEnvironment(server.command),
				args: (server.args ?? []).map(expandEnvironment),
				cwd: server.cwd ? expandPath(server.cwd, cwd) : cwd,
				env: { ...inherited, ...resolveStringMap(server.env) },
				stderr: "pipe",
			});
		}

		try {
			await client.connect(transport, { signal, timeout: startupTimeout });
			const tools: McpTool[] = [];
			let cursor: string | undefined;
			do {
				// Use a lenient result schema because some otherwise compatible servers
				// publish non-standard outputSchema fields. Pi validates inputSchema itself.
				const result = await client.request(
					{ method: "tools/list", params: cursor ? { cursor } : {} },
					LenientListToolsResultSchema,
					{ signal, timeout: startupTimeout },
				);
				tools.push(...(result.tools as McpTool[]));
				cursor = result.nextCursor;
			} while (cursor);
			return { client, transport, tools, cwd };
		} catch (error) {
			await client.close().catch(() => undefined);
			throw error;
		}
	};

	const connectServer = async (name: string, cwd: string, signal?: AbortSignal): Promise<Connection> => {
		const server = config.servers[name];
		if (!server) throw new Error(`Unknown MCP server: ${name}`);
		if (server.enabled === false) throw new Error(`MCP server ${name} is disabled`);

		const existing = connections.get(name);
		if (existing && (server.transport === "http" || existing.cwd === cwd)) return existing;
		if (existing) await closeServer(name);

		const pending = connecting.get(name);
		if (pending) return pending;

		const promise = createConnection(name, server, cwd, signal)
			.then((connection) => {
				connections.set(name, connection);
				connecting.delete(name);
				if (lastContext) updateStatus(lastContext);
				return connection;
			})
			.catch((error) => {
				connecting.delete(name);
				throw new Error(`${name}: ${redactError(error, config)}`);
			});
		connecting.set(name, promise);
		return promise;
	};

	const registerRemoteTool = (serverName: string, remoteTool: McpTool): string => {
		let byRemoteName = registered.get(serverName);
		if (!byRemoteName) {
			byRemoteName = new Map();
			registered.set(serverName, byRemoteName);
		}
		const existing = byRemoteName.get(remoteTool.name);
		if (existing) return existing;

		const name = remoteToolName(serverName, remoteTool.name, usedToolNames);
		usedToolNames.add(name);
		byRemoteName.set(remoteTool.name, name);
		const server = config.servers[serverName];
		if (!server) throw new Error(`Unknown MCP server: ${serverName}`);

		pi.registerTool({
			name,
			label: `${serverName}/${remoteTool.name}`,
			description: `[MCP ${serverName}/${remoteTool.name}] ${remoteTool.description ?? "No description provided."}`,
			parameters: Type.Unsafe<Record<string, unknown>>(remoteTool.inputSchema),
			executionMode: "parallel",
			async execute(_toolCallId, params, signal, _onUpdate, ctx) {
				const connection = await connectServer(serverName, ctx.cwd, signal);
				const timeout =
					server.toolTimeoutMs ?? config.defaults?.toolTimeoutMs ?? DEFAULT_TOOL_TIMEOUT_MS;
				const raw = (await connection.client.callTool(
					{ name: remoteTool.name, arguments: params },
					undefined,
					{ signal, timeout, resetTimeoutOnProgress: true },
				)) as Record<string, unknown>;
				const result = await normalizeToolResult(raw);
				if (result.isError) {
					const message = result.content.find((item) => item.type === "text");
					throw new Error(message?.type === "text" ? message.text : "MCP tool returned an error");
				}
				return {
					content: result.content,
					details: { server: serverName, tool: remoteTool.name },
				};
			},
		});
		return name;
	};

	const activateTools = (names: string[]) => {
		const active = pi.getActiveTools();
		pi.setActiveTools([...new Set([...active, ...names])]);
	};

	const loadServerTools = async (
		serverName: string,
		cwd: string,
		signal?: AbortSignal,
	): Promise<LoaderMatch[]> => {
		const connection = await connectServer(serverName, cwd, signal);
		return connection.tools.map((tool) => ({
			server: serverName,
			tool,
			toolName: registerRemoteTool(serverName, tool),
			score: 1,
		}));
	};

	const enabledServerNames = () =>
		Object.entries(config.servers)
			.filter(([, server]) => server.enabled !== false)
			.map(([name]) => name);

	const formatStatus = () => {
		if (configError) return `MCP config error: ${configError}`;
		const lines = Object.entries(config.servers).map(([name, server]) => {
			const state = server.enabled === false ? "disabled" : connections.has(name) ? "connected" : "idle";
			const toolCount = connections.get(name)?.tools.length;
			return `${name}: ${state}${toolCount === undefined ? "" : ` (${toolCount} tools)`} — ${server.description ?? server.transport}`;
		});
		return lines.length > 0 ? lines.join("\n") : `No MCP servers configured in ${CONFIG_PATH}`;
	};

	pi.registerTool({
		name: "mcp",
		label: "MCP",
		description:
			"Discover and enable tools from configured MCP servers. Use servers to inspect available servers, search to find relevant tools across server catalogs, or load to enable all tools from one server.",
		promptSnippet: "Discover and enable tools from configured MCP servers on demand",
		promptGuidelines: [
			"Use mcp to search for external capabilities when the built-in tools cannot perform the task; MCP tools become callable on the following turn.",
		],
		parameters: Type.Object({
			action: StringEnum(["servers", "search", "load"] as const),
			server: Type.Optional(Type.String({ description: "Optional MCP server name" })),
			query: Type.Optional(Type.String({ description: "Capability or tool to search for" })),
			limit: Type.Optional(Type.Integer({ minimum: 1, maximum: 30 })),
		}),
		executionMode: "sequential",
		async execute(_toolCallId, params, signal, _onUpdate, ctx) {
			lastContext = ctx;
			if (configError) throw new Error(configError);
			if (params.action === "servers") {
				return { content: [{ type: "text", text: formatStatus() }], details: {} };
			}

			if (params.action === "load") {
				if (!params.server) throw new Error("server is required for the load action");
				const matches = await loadServerTools(params.server, ctx.cwd, signal);
				activateTools(matches.map((match) => match.toolName));
				return {
					content: [{
						type: "text",
						text: `Enabled ${matches.length} tools from ${params.server}:\n${matches.map((match) => `- ${match.toolName}: ${match.tool.description ?? match.tool.name}`).join("\n")}`,
					}],
					details: { server: params.server, tools: matches.map((match) => match.toolName) },
				};
			}

			const serverNames = params.server ? [params.server] : enabledServerNames();
			const settled = await Promise.allSettled(
				serverNames.map((serverName) => loadServerTools(serverName, ctx.cwd, signal)),
			);
			const query = params.query ?? "";
			const matches: LoaderMatch[] = [];
			const failures: string[] = [];
			for (let index = 0; index < settled.length; index++) {
				const result = settled[index];
				if (result?.status === "fulfilled") {
					for (const match of result.value) {
						const score = scoreTool(match.tool, query);
						if (score > 0) matches.push({ ...match, score });
					}
				} else if (result?.status === "rejected") {
					failures.push(redactError(result.reason, config));
				}
			}
			matches.sort((left, right) => right.score - left.score || left.toolName.localeCompare(right.toolName));
			const selected = matches.slice(0, params.limit ?? 8);
			activateTools(selected.map((match) => match.toolName));
			const lines = selected.map(
				(match) => `- ${match.toolName}: ${match.tool.description ?? `${match.server}/${match.tool.name}`}`,
			);
			if (failures.length > 0) lines.push(`Unavailable servers:\n${failures.map((failure) => `- ${failure}`).join("\n")}`);
			return {
				content: [{
					type: "text",
					text: selected.length > 0
						? `Enabled ${selected.length} matching MCP tools:\n${lines.join("\n")}`
						: `No matching MCP tools found.${lines.length > 0 ? `\n${lines.join("\n")}` : ""}`,
				}],
				details: { tools: selected.map((match) => match.toolName), failures: failures.length },
			};
		},
	});

	pi.registerCommand("mcp", {
		description: "Manage MCP servers: status, connect, enable, tools, reload",
		getArgumentCompletions(prefix) {
			const parts = prefix.trimStart().split(/\s+/);
			if (parts.length <= 1) {
				return ["status", "connect", "enable", "tools", "reload"]
					.filter((value) => value.startsWith(parts[0] ?? ""))
					.map((value) => ({ value, label: value }));
			}
			const query = parts.at(-1) ?? "";
			return [...enabledServerNames(), "all"]
				.filter((value) => value.startsWith(query))
				.map((value) => ({ value, label: value }));
		},
		async handler(args, ctx) {
			lastContext = ctx;
			const [action = "status", target] = args.trim().split(/\s+/, 2);
			if (action === "reload") {
				await closeAll();
				await loadConfig();
				updateStatus(ctx);
				ctx.ui.notify(configError ? `MCP reload failed: ${configError}` : "MCP configuration reloaded", configError ? "error" : "info");
				return;
			}
			if (action === "status") {
				ctx.ui.notify(formatStatus(), configError ? "error" : "info");
				return;
			}
			if (!["connect", "enable", "tools"].includes(action)) {
				ctx.ui.notify("Usage: /mcp [status|connect <server|all>|enable <server|all>|tools <server>|reload]", "error");
				return;
			}
			if (!target) {
				ctx.ui.notify(`/mcp ${action} requires a server name`, "error");
				return;
			}
			const names = target === "all" ? enabledServerNames() : [target];
			const settled = await Promise.allSettled(names.map((name) => loadServerTools(name, ctx.cwd)));
			const loaded = settled.flatMap((result) => (result.status === "fulfilled" ? result.value : []));
			const failures = settled.flatMap((result) =>
				result.status === "rejected" ? [redactError(result.reason, config)] : [],
			);
			if (action === "enable") activateTools(loaded.map((match) => match.toolName));
			if (action === "tools") {
				ctx.ui.notify(
					loaded.length > 0 ? loaded.map((match) => `${match.toolName} — ${match.tool.description ?? match.tool.name}`).join("\n") : "No tools found",
					loaded.length > 0 ? "info" : "warning",
				);
			} else {
				ctx.ui.notify(
					`${action === "enable" ? "Enabled" : "Connected"} ${loaded.length} tools across ${names.length - failures.length} server(s)${failures.length > 0 ? `; ${failures.length} failed` : ""}`,
					failures.length > 0 ? "warning" : "info",
				);
			}
			if (failures.length > 0) ctx.ui.notify(failures.join("\n"), "warning");
			updateStatus(ctx);
		},
	});

	pi.on("session_start", async (_event, ctx) => {
		lastContext = ctx;
		for (const tool of pi.getAllTools()) usedToolNames.add(tool.name);
		await loadConfig();
		updateStatus(ctx);
		if (configError) ctx.ui.notify(`MCP bridge: ${configError}`, "error");
	});

	pi.on("session_shutdown", async () => {
		await closeAll();
		lastContext = undefined;
	});
}
