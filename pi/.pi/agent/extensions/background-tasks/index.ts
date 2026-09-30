import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { Text } from "@earendil-works/pi-tui";
import { Type } from "typebox";
import { isAbsolute, resolve } from "node:path";
import { cleanOutput, describeTask, TaskManager, type TaskRecord } from "./manager.js";

const ENTRY = "po-background-task";
const MESSAGE = "po-background-task-completion";
const IdParams = Type.Object({ id: Type.String({ description: "Task ID returned by task_start" }) });

export default function backgroundTasks(pi: ExtensionAPI) {
	let manager: TaskManager | undefined;
	let context: ExtensionContext | undefined;
	let closing = false;
	const updateStatus = () => {
		if (!context?.hasUI) return;
		const count = manager?.list().filter((r) => r.status === "running" || r.status === "stopping").length ?? 0;
		context.ui.setStatus("background-tasks", count ? `${count} task${count === 1 ? "" : "s"}` : undefined);
	};
	const persist = (record: TaskRecord) => {
		pi.appendEntry(ENTRY, { ...record, output: cleanOutput(record.output.slice(-16000)) });
	};
	const getManager = (ctx: ExtensionContext) => {
		if (closing || !manager) throw new Error("Background task session is not active.");
		context = ctx;
		return manager;
	};
	const result = (record: TaskRecord, tailChars = 4000) => ({
		content: [{ type: "text" as const, text: describeTask(record, tailChars) }],
		details: { ...record, output: tailChars > 0 ? cleanOutput(record.output.slice(-tailChars)) : "" },
	});

	pi.on("session_start", (_event, ctx) => {
		closing = false;
		context = ctx;
		manager = new TaskManager((record) => {
			try {
				persist(record);
				updateStatus();
				if (!closing && record.notify) {
					pi.sendMessage({ customType: MESSAGE, content: `Background task finished.\n${describeTask(record)}`,
						display: true, details: { id: record.id, status: record.status } },
					{ deliverAs: "followUp", triggerTurn: true });
				}
			} catch (error) {
				if (!closing && ctx.hasUI) ctx.ui.notify(`Could not report task completion: ${String(error)}`, "error");
			}
		});
		// Jobs are real session resources, not branch-sensitive hypothetical state.
		const saved = new Map<string, TaskRecord>();
		for (const entry of ctx.sessionManager.getEntries()) {
			if (entry.type !== "custom" || entry.customType !== ENTRY) continue;
			const record = entry.data as TaskRecord | undefined;
			if (record && typeof record.id === "string" && typeof record.output === "string" && typeof record.command === "string") {
				saved.set(record.id, record);
			}
		}
		manager.restore([...saved.values()]);
		updateStatus();
	});
	pi.on("session_shutdown", async () => {
		closing = true;
		await manager?.shutdown();
		context?.ui.setStatus("background-tasks", undefined);
		manager = undefined;
		context = undefined;
	});

	pi.registerTool({
		name: "task_start", label: "Start background task",
		description: "Start a managed background shell command and return its task ID immediately. Use for builds, tests, dev servers, and log streams while doing other work. Runs bash -c with inherited environment, no stdin, in its own process group. Jobs stop on session exit/reload/switch. Do not daemonize, use &, or detach descendants. Set notify=false for servers/log streams that should not trigger an agent turn on completion. Output is a bounded tail, not a full log archive.",
		parameters: Type.Object({
			command: Type.String({ minLength: 1, maxLength: 16000 }),
			cwd: Type.Optional(Type.String({ description: "Working directory; relative paths resolve against the session directory" })),
			label: Type.Optional(Type.String({ maxLength: 120, description: "Short human-readable name" })),
			notify: Type.Optional(Type.Boolean({ description: "Send a completion event and wake the agent. Default true; use false for long-running servers." })),
		}),
		executionMode: "sequential",
		async execute(_id, params, signal, _update, ctx) {
			if (signal?.aborted) throw new Error("Task start cancelled.");
			const tasks = getManager(ctx);
			const cwd = params.cwd ? (isAbsolute(params.cwd) ? params.cwd : resolve(ctx.cwd, params.cwd)) : ctx.cwd;
			const record = await tasks.start(params.command, cwd, params.label ?? "", params.notify ?? true);
			persist(record); updateStatus();
			return result(record, 0);
		},
	});
	pi.registerTool({
		name: "task_list", label: "List background tasks",
		description: "List this session's active jobs and jobs finished within the last minute. Older jobs remain accessible by ID. Not shared with other Po sessions.",
		parameters: Type.Object({}),
		async execute(_id, _params, _signal, _update, ctx) {
			const records = getManager(ctx).listVisible();
			const tasks = records.map(({ output, ...record }) => ({ ...record, outputChars: output.length }));
			return { content: [{ type: "text", text: tasks.length ? tasks.map((r) => `${r.id} · ${r.status} · ${cleanOutput(r.label)}`).join("\n") : "No background tasks." }], details: { tasks } };
		},
	});
	pi.registerTool({
		name: "task_output", label: "Read background task output",
		description: "Read a bounded tail of combined stdout/stderr and the current status of a managed background task.",
		parameters: Type.Object({ ...IdParams.properties,
			tail_chars: Type.Optional(Type.Integer({ minimum: 1, maximum: 16000, description: "Tail size; default 4000, maximum 16000" })) }),
		async execute(_id, params, _signal, _update, ctx) {
			return result(getManager(ctx).get(params.id), params.tail_chars ?? 4000);
		},
	});
	pi.registerTool({
		name: "task_wait", label: "Wait for background task",
		description: "Wait up to timeout_seconds for a managed job to finish; returns current status and output even on timeout. Cancelling the wait leaves the job running; use task_stop to terminate it.",
		parameters: Type.Object({ ...IdParams.properties,
			timeout_seconds: Type.Optional(Type.Number({ minimum: 0, maximum: 60, description: "Maximum wait; default 10 seconds" })) }),
		async execute(_id, params, signal, _update, ctx) {
			return result(await getManager(ctx).wait(params.id, (params.timeout_seconds ?? 10) * 1000, signal));
		},
	});
	pi.registerTool({
		name: "task_stop", label: "Stop background task",
		description: "Stop a managed shell job and its process group. Sends SIGTERM, escalating to SIGKILL after one second. Already-finished tasks are unchanged.",
		parameters: IdParams,
		async execute(_id, params, _signal, _update, ctx) { return result(await getManager(ctx).stop(params.id)); },
	});

	pi.registerMessageRenderer(MESSAGE, (message, _options, theme) =>
		new Text(theme.fg("muted", typeof message.content === "string" ? message.content : "Background task finished"), 0, 0));

	pi.registerCommand("tasks", {
		description: "Inspect background tasks: /tasks, /tasks <id>, /tasks stop <id>",
		async handler(args, ctx) {
			try {
				const tasks = getManager(ctx);
				const parts = args.trim().split(/\s+/).filter(Boolean);
				if (parts[0] === "stop" && parts.length === 2) {
					const record = await tasks.stop(parts[1]);
					if (ctx.hasUI) ctx.ui.notify(`${record.id}: ${record.status}`, "info");
					return;
				}
				if (parts.length > 1) throw new Error("Usage: /tasks [<id> | stop <id>]");
				let id: string | undefined = parts[0];
				if (!id && ctx.hasUI) {
					const records = tasks.listVisible();
					if (!records.length) { ctx.ui.notify("No background tasks.", "info"); return; }
					const selected = await ctx.ui.select("Background tasks", records.map((r) => `${r.id} · ${r.status} · ${cleanOutput(r.label)}`));
					id = selected?.split(" · ")[0];
				}
				if (!id) return;
				const record = tasks.get(id);
				if (!ctx.hasUI) return;
				const action = await ctx.ui.select(describeTask(record), ["Refresh output", "Stop task", "Close"]);
				if (action === "Stop task" && await ctx.ui.confirm("Stop background task?", cleanOutput(record.label))) {
					await tasks.stop(id);
					ctx.ui.notify(`${id}: ${tasks.get(id).status}`, "info");
				} else if (action === "Refresh output") {
					ctx.ui.notify(describeTask(tasks.get(id)), "info");
				}
			} catch (error) { if (ctx.hasUI) ctx.ui.notify(String(error), "error"); }
		},
	});
}
