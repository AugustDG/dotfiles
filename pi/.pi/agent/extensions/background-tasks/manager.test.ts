import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { cleanOutput, commandSnippet, describeTask, formatTaskDuration, OUTPUT_LIMIT, TaskManager, type TaskRecord } from "./manager.ts";

const cwd = process.cwd();
async function finished(manager: TaskManager, id: string) {
	const task = await manager.wait(id, 5000);
	assert.notEqual(task.status, "running");
	return task;
}
function isRunning(pid: number): boolean {
	try { return !execFileSync("ps", ["-o", "stat=", "-p", String(pid)], { encoding: "utf8" }).trim().startsWith("Z"); }
	catch { return false; }
}

test("returns immediately, captures both streams, cwd, and exit code", async (t) => {
	const completions: TaskRecord[] = [];
	const manager = new TaskManager((r) => completions.push(r));
	t.after(() => manager.shutdown());
	const started = await manager.start("sleep 0.15; pwd; printf out; printf err >&2; exit 7", cwd, "test");
	assert.equal(started.status, "running");
	const task = await finished(manager, started.id);
	assert.equal(task.status, "failed");
	assert.equal(task.exitCode, 7);
	assert.ok(task.output.includes(cwd));
	assert.ok(task.output.includes("out"));
	assert.ok(task.output.includes("err"));
	assert.equal(completions.length, 1);
	assert.equal((await manager.stop(started.id)).status, "failed");
});

test("bounds output and strips terminal controls", async (t) => {
	const manager = new TaskManager();
	t.after(() => manager.shutdown());
	const started = await manager.start(`head -c ${OUTPUT_LIMIT + 10000} /dev/zero | tr '\\0' x; printf END`, cwd, "large");
	const task = await finished(manager, started.id);
	assert.equal(task.output.length, OUTPUT_LIMIT);
	assert.ok(task.droppedChars >= 10000);
	assert.ok(task.output.endsWith("END"));
	assert.ok(describeTask(task, 10).includes("Output (snippet)"));
	assert.ok(!describeTask(task, 0).includes("Output:"));
	assert.equal(cleanOutput("\x1b[31mred\x1b[0m\x1b]52;c;bad\x07\x00"), "red");
});

test("task descriptions use command/output snippets, exit and duration, without directory", () => {
	const record: TaskRecord = {
		id: "snippet", command: `printf '${"x".repeat(250)}'\n; echo done`, cwd: "/private/directory",
		label: "test", notify: false, status: "completed", startedAt: 1000, endedAt: 11_017,
		exitCode: 0, output: Array.from({ length: 30 }, (_, i) => `line ${i}`).join("\n"), droppedChars: 0,
	};
	const text = describeTask(record);
	assert.ok(!text.includes("Directory:"));
	assert.ok(!text.includes(record.cwd));
	assert.ok(text.includes("Exit: 0 · Duration: 10s"));
	assert.equal(text.split("\n").find((line) => line.startsWith("Command: "))!.slice(9).length, 160);
	assert.ok(text.includes("Output (snippet):\n…\nline 22"));
	assert.ok(!text.includes("\nline 21\n"));
	assert.ok(text.endsWith("line 29"));
	assert.ok(describeTask(record, 16000).includes("\nline 0\n"));
	assert.equal(commandSnippet("echo a\n\t; echo b"), "echo a ; echo b");
	assert.equal(formatTaskDuration(45), "45ms");
	assert.equal(formatTaskDuration(1250), "1.3s");
	assert.equal(formatTaskDuration(62_000), "1m 2s");
	assert.ok(describeTask({ ...record, exitCode: null, signal: "SIGTERM" }).includes("Exit: SIGTERM"));
	const longOutput = describeTask({ ...record, output: "y".repeat(2000) });
	assert.ok(longOutput.endsWith("y".repeat(500)));
	assert.ok(!longOutput.endsWith("y".repeat(501)));
});

test("timeout and cancellation do not terminate the job", async (t) => {
	const manager = new TaskManager();
	t.after(() => manager.shutdown());
	const started = await manager.start("sleep 30", cwd, "sleep");
	assert.equal((await manager.wait(started.id, 10)).status, "running");
	const controller = new AbortController();
	const waiting = manager.wait(started.id, 5000, controller.signal);
	controller.abort();
	await assert.rejects(waiting, /still running/);
	assert.equal(manager.get(started.id).status, "running");
	assert.equal((await manager.stop(started.id)).status, "stopped");
});

test("stop escalates and terminates children even if they ignore TERM", async (t) => {
	const manager = new TaskManager();
	t.after(() => manager.shutdown());
	const started = await manager.start("trap '' TERM; bash -c 'trap \"\" TERM; echo $$; while :; do sleep 1; done' & wait", cwd, "tree");
	for (let i = 0; i < 100 && !manager.get(started.id).output.trim(); i++) await new Promise((r) => setTimeout(r, 10));
	const pid = Number(manager.get(started.id).output.trim());
	assert.ok(pid > 0);
	assert.equal(isRunning(pid), true);
	assert.equal((await manager.stop(started.id)).status, "stopped");
	assert.equal(isRunning(pid), false);
});

test("natural shell exit cleans up leftover descendants", async (t) => {
	const manager = new TaskManager();
	t.after(() => manager.shutdown());
	const started = await manager.start("sleep 30 & echo $!", cwd, "leftover");
	const task = await finished(manager, started.id);
	assert.equal(task.status, "completed");
	assert.equal(isRunning(Number(task.output.trim())), false);
});

test("shutdown is idempotent and blocks further starts", async () => {
	const manager = new TaskManager();
	const task = await manager.start("sleep 30", cwd, "sleep");
	await Promise.all([manager.shutdown(), manager.shutdown()]);
	assert.equal(manager.get(task.id).status, "stopped");
	await assert.rejects(manager.start("true", cwd, "new"), /shutting down/);
});

test("restore marks live records interrupted and never signals saved PIDs", async (t) => {
	const old = new TaskManager();
	t.after(() => old.shutdown());
	const started = await old.start("sleep 30", cwd, "old");
	const restored = new TaskManager();
	restored.restore([started]);
	assert.equal(restored.get(started.id).status, "interrupted");
	assert.equal((await restored.stop(started.id)).status, "interrupted");
	assert.equal(old.get(started.id).status, "running");
});

test("visible list hides terminal jobs older than one minute but preserves lookup and active jobs", async (t) => {
	const manager = new TaskManager();
	t.after(() => manager.shutdown());
	const active = await manager.start("sleep 30", cwd, "active");
	const now = Date.now() + 120_000;
	manager.restore([
		{ ...active, id: "recent", status: "completed", endedAt: now - 59_999 },
		{ ...active, id: "boundary", status: "failed", endedAt: now - 60_000 },
		{ ...active, id: "old", status: "completed", endedAt: now - 60_001 },
		{ ...active, id: "stopped", status: "stopped", endedAt: now - 60_001 },
		{ ...active, id: "interrupted", status: "interrupted", endedAt: now - 60_001 },
	]);
	assert.deepEqual(manager.listVisible(now).map((r) => r.id), [active.id, "recent", "boundary"]);
	assert.equal(manager.get("old").status, "completed");
	assert.equal(manager.list().length, 6);
});

test("invalid cwd, unknown task, spawn failure, and concurrency limit", async (t) => {
	const manager = new TaskManager();
	t.after(() => manager.shutdown());
	await assert.rejects(manager.start("true", "/nonexistent-po-background-dir", "bad"));
	assert.throws(() => manager.get("missing"), /Unknown task/);
	const badShell = new TaskManager(() => {}, "/nonexistent-po-shell");
	await assert.rejects(badShell.start("true", cwd, "bad"));
	await badShell.shutdown();
	const attempts = await Promise.allSettled(Array.from({ length: 10 }, () => manager.start("sleep 30", cwd, "limit")));
	assert.equal(attempts.filter((r) => r.status === "fulfilled").length, 8);
	assert.equal(attempts.filter((r) => r.status === "rejected").length, 2);
});
