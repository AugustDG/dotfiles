import { test } from "node:test";
import assert from "node:assert/strict";
import { join } from "node:path";

// Load exactly as Pi does, with its installed runtime dependencies.
const modules = process.env.NODE_PATH;
if (!modules) throw new Error("Set NODE_PATH to Pi's global node_modules directory.");
const { createJiti } = await import(join(modules, "jiti/lib/jiti.mjs"));
const jiti = createJiti(import.meta.url, {
	alias: { typebox: join(modules, "typebox/build/index.mjs"),
		"@earendil-works/pi-tui": join(modules, "@earendil-works/pi-tui/dist/index.js") },
});
const extension = await jiti.import("./index.ts", { default: true });

function setup(saved: any[] = []) {
	const handlers = new Map<string, Function>();
	const tools = new Map<string, any>();
	const commands = new Map<string, any>();
	const entries = [...saved];
	const messages: any[] = [];
	const ctx: any = {
		cwd: process.cwd(), hasUI: false, mode: "json",
		sessionManager: { getEntries: () => entries },
		ui: { setStatus() {}, notify() { throw new Error("Unexpected UI"); } },
	};
	extension({
		on: (name: string, handler: Function) => handlers.set(name, handler),
		registerTool: (tool: any) => tools.set(tool.name, tool),
		registerCommand: (name: string, command: any) => commands.set(name, command),
		registerMessageRenderer() {},
		appendEntry: (customType: string, data: any) => entries.push({ type: "custom", customType, data }),
		sendMessage: (message: any, options: any) => messages.push({ message, options }),
	});
	handlers.get("session_start")!({}, ctx);
	return { handlers, entries, messages, ctx, commands,
		call: (name: string, params: any) => tools.get(name).execute("call", params, undefined, undefined, ctx),
		shutdown: () => handlers.get("session_shutdown")!({}, ctx),
	};
}

test("extension registers working tools, persists results, and emits one completion event", async (t) => {
	const h = setup();
	t.after(h.shutdown);
	const started = await h.call("task_start", { command: "sleep 0.05; printf success", label: "test" });
	const id = started.details.id;
	const waited = await h.call("task_wait", { id, timeout_seconds: 2 });
	assert.equal(waited.details.status, "completed");
	assert.equal(waited.details.output, "success");
	assert.equal(h.messages.length, 1);
	assert.ok(!h.messages[0].message.content.includes("Directory:"));
	assert.ok(h.messages[0].message.content.includes("Exit: 0 · Duration:"));
	assert.deepEqual(h.messages[0].options, { deliverAs: "followUp", triggerTurn: true });
	assert.equal(h.entries.at(-1).data.status, "completed");
	assert.equal((await h.call("task_list", {})).details.tasks.length, 1);
	assert.equal((await h.call("task_output", { id, tail_chars: 3 })).details.output, "ess");
});

test("notify=false suppresses completion turns and shutdown stops jobs silently", async () => {
	const h = setup();
	try {
		const quiet = await h.call("task_start", { command: "true", notify: false });
		await h.call("task_wait", { id: quiet.details.id, timeout_seconds: 2 });
		assert.equal(h.messages.length, 0);
		const long = await h.call("task_start", { command: "sleep 30" });
		await h.shutdown();
		assert.equal(h.entries.at(-1).data.id, long.details.id);
		assert.equal(h.entries.at(-1).data.status, "stopped");
		assert.equal(h.messages.length, 0);
		await assert.rejects(h.call("task_list", {}), /not active/);
	} finally { await h.shutdown(); }
});

test("resumed session restores history without resurrecting jobs or replaying notifications", async (t) => {
	const h = setup();
	t.after(h.shutdown);
	const task = await h.call("task_start", { command: "sleep 30" });
	const restored = setup(h.entries);
	t.after(restored.shutdown);
	assert.equal((await restored.call("task_output", { id: task.details.id })).details.status, "interrupted");
	assert.equal(restored.messages.length, 0);
	assert.equal((await h.call("task_output", { id: task.details.id })).details.status, "running");
});

test("old completed jobs are hidden from tool and UI lists but still readable by ID", async (t) => {
	const record = {
		id: "old", command: "true", cwd: process.cwd(), label: "old job", notify: false,
		status: "completed", startedAt: Date.now() - 120_000, endedAt: Date.now() - 90_000,
		exitCode: 0, output: "saved output", droppedChars: 0,
	};
	const h = setup([{ type: "custom", customType: "po-background-task", data: record }]);
	t.after(h.shutdown);
	assert.equal((await h.call("task_list", {})).details.tasks.length, 0);
	assert.equal((await h.call("task_output", { id: "old" })).details.output, "saved output");
	h.ctx.hasUI = true;
	const notices: string[] = [];
	h.ctx.ui.notify = (message: string) => notices.push(message);
	h.ctx.ui.select = async () => { throw new Error("Old task should not be selectable"); };
	await h.commands.get("tasks").handler("", h.ctx);
	assert.deepEqual(notices, ["No background tasks."]);
});

test("output defaults to a compact snippet while explicit larger tails stay available", async (t) => {
	const record = {
		id: "large", command: `echo ${"x".repeat(300)}`, cwd: process.cwd(), label: "large", notify: false,
		status: "completed", startedAt: Date.now() - 10_000, endedAt: Date.now(), exitCode: 0,
		output: "a".repeat(2000), droppedChars: 0,
	};
	const h = setup([{ type: "custom", customType: "po-background-task", data: record }]);
	t.after(h.shutdown);
	const snippet = await h.call("task_output", { id: record.id });
	assert.equal(snippet.details.output.length, 500);
	assert.ok(snippet.content[0].text.includes("Output (snippet)"));
	assert.ok(!snippet.content[0].text.includes(record.command));
	assert.ok(!snippet.content[0].text.includes("Directory:"));
	const larger = await h.call("task_output", { id: record.id, tail_chars: 4000 });
	assert.equal(larger.details.output.length, 2000);
});

test("bottom-bar status uses short task counts and clears at zero", async (t) => {
	const h = setup();
	t.after(h.shutdown);
	h.ctx.hasUI = true;
	const statuses: any[] = [];
	h.ctx.ui.setStatus = (key: string, value: string | undefined) => statuses.push([key, value]);
	const first = await h.call("task_start", { command: "sleep 30", notify: false });
	assert.deepEqual(statuses.at(-1), ["background-tasks", "1 task"]);
	const second = await h.call("task_start", { command: "sleep 30", notify: false });
	assert.deepEqual(statuses.at(-1), ["background-tasks", "2 tasks"]);
	await h.call("task_stop", { id: first.details.id });
	assert.deepEqual(statuses.at(-1), ["background-tasks", "1 task"]);
	await h.call("task_stop", { id: second.details.id });
	assert.deepEqual(statuses.at(-1), ["background-tasks", undefined]);
});

test("UI command can inspect and stop a task via standard dialogs", async (t) => {
	const h = setup();
	t.after(h.shutdown);
	const task = await h.call("task_start", { command: "sleep 30", notify: false });
	const selections = [`${task.details.id} · running · test`, "Stop task"];
	const notices: string[] = [];
	h.ctx.hasUI = true;
	h.ctx.ui.select = async () => selections.shift();
	h.ctx.ui.confirm = async () => true;
	h.ctx.ui.notify = (message: string) => notices.push(message);
	await h.commands.get("tasks").handler("", h.ctx);
	assert.equal((await h.call("task_output", { id: task.details.id })).details.status, "stopped");
	assert.ok(notices[0].includes("stopped"));
});
