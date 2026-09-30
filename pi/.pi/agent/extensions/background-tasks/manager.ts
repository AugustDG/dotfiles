import { spawn, type ChildProcess } from "node:child_process";
import { randomUUID } from "node:crypto";
import { stat } from "node:fs/promises";

export type TaskStatus = "running" | "stopping" | "completed" | "failed" | "stopped" | "interrupted";
export interface TaskRecord {
	id: string;
	command: string;
	cwd: string;
	label: string;
	notify: boolean;
	status: TaskStatus;
	startedAt: number;
	endedAt?: number;
	exitCode?: number | null;
	signal?: string | null;
	output: string;
	droppedChars: number;
}
interface LiveTask {
	record: TaskRecord;
	child: ChildProcess;
	done: Promise<void>;
	resolve: () => void;
	waiters: Set<() => void>;
	killTimer?: ReturnType<typeof setTimeout>;
}
export const OUTPUT_LIMIT = 256 * 1024;
const MAX_TASKS = 100;
const MAX_RUNNING = 8;

export function cleanOutput(text: string): string {
	// Logs are untrusted terminal input: remove ANSI/OSC and other controls.
	return text.replace(/\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)/g, "")
		.replace(/\x1b\[[0-?]*[ -/]*[@-~]/g, "")
		.replace(/[\x00-\x08\x0b-\x1f\x7f-\x9f]/g, "");
}

export class TaskManager {
	private records = new Map<string, TaskRecord>();
	private live = new Map<string, LiveTask>();
	private closing = false;
	private pendingStarts = 0;
	private onFinish: (record: TaskRecord) => void;
	private shell: string;
	constructor(onFinish: (record: TaskRecord) => void = () => {}, shell = "/bin/bash") {
		this.onFinish = onFinish;
		this.shell = shell;
	}

	list(): TaskRecord[] { return [...this.records.values()].map((r) => ({ ...r })); }
	listVisible(now = Date.now()): TaskRecord[] {
		return this.list().filter((record) =>
			record.status === "running" || record.status === "stopping" ||
			now - (record.endedAt ?? record.startedAt) <= 60_000,
		);
	}
	get(id: string): TaskRecord {
		const record = this.records.get(id);
		if (!record) throw new Error(`Unknown task: ${id}`);
		return { ...record };
	}
	restore(records: TaskRecord[]): void {
		for (const saved of records.slice(-MAX_TASKS)) {
			const record = { ...saved, output: saved.output.slice(-OUTPUT_LIMIT) };
			if (record.status === "running" || record.status === "stopping") {
				record.status = "interrupted";
				record.endedAt = Date.now();
			}
			this.records.set(record.id, record);
		}
	}

	async start(command: string, cwd: string, label: string, notify = true): Promise<TaskRecord> {
		if (process.platform === "win32") throw new Error("Background tasks currently require macOS or Linux.");
		if (this.closing) throw new Error("Task manager is shutting down.");
		if (!command.trim()) throw new Error("Command cannot be empty.");
		if (this.live.size + this.pendingStarts >= MAX_RUNNING) throw new Error(`At most ${MAX_RUNNING} tasks may run at once.`);
		this.pendingStarts++;
		try {
			if (!(await stat(cwd)).isDirectory()) throw new Error(`Not a directory: ${cwd}`);
			if (this.closing) throw new Error("Task manager is shutting down.");
			while (this.records.size >= MAX_TASKS) {
				const oldest = [...this.records.keys()].find((id) => !this.live.has(id));
				if (!oldest) throw new Error("Task history is full.");
				this.records.delete(oldest);
			}
			const record: TaskRecord = {
				id: randomUUID().slice(0, 8), command, cwd, label: label || command.slice(0, 80), notify,
				status: "running", startedAt: Date.now(), output: "", droppedChars: 0,
			};
			const child = spawn(this.shell, ["-c", command], {
				cwd, detached: true, stdio: ["ignore", "pipe", "pipe"],
			});
			let resolve!: () => void;
			const done = new Promise<void>((r) => { resolve = r; });
			const task: LiveTask = { record, child, done, resolve, waiters: new Set() };
			this.records.set(record.id, record);
			this.live.set(record.id, task);
			const append = (chunk: string) => {
				record.output += chunk;
				const excess = record.output.length - OUTPUT_LIMIT;
				if (excess > 0) { record.output = record.output.slice(excess); record.droppedChars += excess; }
			};
			child.stdout!.setEncoding("utf8").on("data", append);
			child.stderr!.setEncoding("utf8").on("data", append);
			child.on("error", (error) => { append(`\n${error.message}\n`); });
			child.on("exit", () => {
				// A shell exiting must not leave descendants running or holding its output pipes open.
				if (record.status === "running") this.killGroup(task, "SIGKILL");
			});
			child.once("close", (code, signal) => {
				if (task.killTimer) { clearTimeout(task.killTimer); this.killGroup(task, "SIGKILL"); }
				record.status = record.status === "stopping" ? "stopped" : code === 0 ? "completed" : "failed";
				record.exitCode = code; record.signal = signal; record.endedAt = Date.now();
				this.live.delete(record.id);
				resolve();
				for (const waiter of [...task.waiters]) waiter();
				this.onFinish({ ...record });
			});
			await new Promise<void>((resolve, reject) => { child.once("spawn", resolve); child.once("error", reject); });
			return this.get(record.id);
		} finally { this.pendingStarts--; }
	}

	private killGroup(task: LiveTask, signal: NodeJS.Signals): void {
		if (!task.child.pid) return;
		try { process.kill(-task.child.pid, signal); }
		catch (error) { if ((error as NodeJS.ErrnoException).code !== "ESRCH") throw error; }
	}

	async stop(id: string): Promise<TaskRecord> {
		this.get(id);
		const task = this.live.get(id);
		if (!task) return this.get(id);
		if (task.record.status !== "stopping") {
			task.record.status = "stopping";
			this.killGroup(task, "SIGTERM");
			task.killTimer = setTimeout(() => this.killGroup(task, "SIGKILL"), 1000);
		}
		await task.done;
		return this.get(id);
	}

	async wait(id: string, timeoutMs: number, signal?: AbortSignal): Promise<TaskRecord> {
		this.get(id);
		const task = this.live.get(id);
		if (!task) return this.get(id);
		if (signal?.aborted) throw new Error("Wait cancelled; the background task is still running.");
		await new Promise<void>((resolve, reject) => {
			const finish = () => { cleanup(); resolve(); };
			const abort = () => { cleanup(); reject(new Error("Wait cancelled; the background task is still running.")); };
			const timer = setTimeout(finish, timeoutMs);
			const cleanup = () => { clearTimeout(timer); signal?.removeEventListener("abort", abort); task.waiters.delete(finish); };
			signal?.addEventListener("abort", abort, { once: true });
			task.waiters.add(finish);
		});
		return this.get(id);
	}

	async shutdown(): Promise<void> {
		this.closing = true;
		await Promise.all([...this.live.keys()].map((id) => this.stop(id)));
	}
}

export function describeTask(record: TaskRecord, tailChars = 4000): string {
	const tail = tailChars > 0 ? cleanOutput(record.output.slice(-tailChars)) : "";
	return [
		`${record.id} · ${cleanOutput(record.label)} · ${record.status}`,
		`Command: ${cleanOutput(record.command)}\nDirectory: ${cleanOutput(record.cwd)}`,
		record.endedAt ? `Exit: ${record.exitCode ?? record.signal ?? "unknown"} · ${record.endedAt - record.startedAt}ms` : "",
		record.droppedChars || record.output.length > tailChars ? "Output truncated (bounded tail only)." : "",
		tail ? `--- output (stdout + stderr) ---\n${tail}` : "(no output)",
	].filter(Boolean).join("\n");
}
