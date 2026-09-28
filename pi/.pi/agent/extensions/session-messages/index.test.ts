import { afterAll, expect, test, spyOn } from "bun:test";
import { mkdtemp, mkdir, readdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { SessionManager } from "@earendil-works/pi-coding-agent";

const dir = await mkdtemp(join(tmpdir(), "po-messages-test-"));
const previousDir = process.env.PI_CODING_AGENT_DIR;
process.env.PI_CODING_AGENT_DIR = dir;
const { default: extension, getPoName } = await import("./index");
const list = spyOn(SessionManager, "listAll").mockResolvedValue([]);
afterAll(async () => {
	list.mockRestore();
	if (previousDir === undefined) delete process.env.PI_CODING_AGENT_DIR;
	else process.env.PI_CODING_AGENT_DIR = previousDir;
	await rm(dir, { recursive: true, force: true });
});

const theme = { fg: (_: string, text: string) => text };
function harness(id = "session-12345678", initialName?: string, poName?: string) {
	let name = initialName;
	const handlers: Record<string, Function> = {};
	const commands: Record<string, any> = {};
	const renderers: Record<string, any> = {};
	const entries: any[] = poName ? [{ type: "custom", customType: "po-identity", data: { sessionId: id, name: poName } }] : [];
	let tool: any;
	const pi = {
		on: (event: string, handler: Function) => { handlers[event] = handler; },
		registerTool: (value: any) => { tool = value; },
		registerCommand: (key: string, value: any) => { commands[key] = value; },
		registerEntryRenderer: (key: string, value: any) => { renderers[key] = value; },
		appendEntry: (customType: string, data: any) => entries.push({ type: "custom", customType, data }),
		getSessionName: () => name,
		setSessionName: (value: string) => { name = value; },
		sendMessage: () => {},
	};
	const ctx = {
		cwd: dir,
		sessionManager: { getEntries: () => entries, getSessionId: () => id, getSessionFile: () => undefined, getSessionDir: () => dir },
		ui: { notify: (text: string) => { throw new Error(text); } },
	};
	extension(pi as any);
	return { tool, commands, renderers, entries, ctx, name: () => getPoName(ctx as any), title: () => name,
		prompt: () => handlers.before_agent_start({ systemPrompt: "Base instructions" }),
		start: () => handlers.session_start({}, ctx), stop: () => handlers.session_shutdown() };
}

const peer = { id: "peer-87654321", name: "A task title", cwd: dir, path: join(dir, "peer.jsonl"), modified: new Date() };
await writeFile(peer.path, JSON.stringify({ type: "custom", customType: "po-identity", data: { sessionId: peer.id, name: "Christina Po" } }));
const duplicatePath = join(dir, "duplicate.jsonl");
await writeFile(duplicatePath, JSON.stringify({ type: "custom", customType: "po-identity", data: { sessionId: "peer-11223344", name: "Christina Po" } }));

async function publishPeer(id = peer.id, lastSeen = new Date().toISOString(), pid = process.pid) {
	const presenceDir = join(dir, "session-messages/presence");
	await mkdir(presenceDir, { recursive: true });
	await writeFile(join(presenceDir, `${id}.${pid}.json`), JSON.stringify({
		version: 1, sessionId: id, sessionFile: peer.path, name: "Christina Po", cwd: dir,
		pid, startedAt: lastSeen, lastSeen,
	}));
}
await publishPeer();

test("unnamed sessions get stable first names; explicit names survive", async () => {
	const first = harness();
	await first.start();
	expect(first.name()).toMatch(/^[A-Z][a-z]+ Po$/);
	await first.stop();
	const resumed = harness();
	await resumed.start();
	expect(resumed.name()).toBe(first.name());
	await resumed.stop();
	const named = harness("session-12345678", "My project");
	await named.start();
	expect(named.title()).toBe("My project");
	expect(named.name()).toBe(first.name());
	await named.stop();
});

test("send by name queues full body and expanded tool output reveals it", async () => {
	list.mockResolvedValue([peer] as any);
	const h = harness();
	await h.start();
	try {
		const args = { action: "send", target: "christina po", message: "Hello\nFull message body" };
		const result = await h.tool.execute("call", args);
		const collapsed = h.tool.renderResult(result, { expanded: false }, theme, { args }).render(100).join("\n");
		const expanded = h.tool.renderResult(result, { expanded: true }, theme, { args }).render(100).join("\n");
		expect(collapsed).not.toContain("Full message body");
		expect(expanded).toContain("Full message body");
		const inbox = join(dir, "session-messages/inbox", peer.id);
		const files = await readdir(inbox);
		const envelope = JSON.parse(await readFile(join(inbox, files[0]!), "utf8"));
		expect(envelope.message).toBe(args.message);
		expect(envelope.fromSessionName).toBe(h.name());
	} finally { await h.stop(); }
});

test("duplicate names require ID; listing is name-first", async () => {
	await publishPeer("peer-11223344");
	list.mockResolvedValue([peer, { ...peer, id: "peer-11223344", path: duplicatePath }] as any);
	const h = harness();
	await h.start();
	try {
		await expect(h.tool.execute("call", { action: "send", target: "Christina Po", message: "hello" })).rejects.toThrow("multiple sessions");
		await expect(h.tool.execute("call", { action: "send", target: "87654321", message: "hello" })).resolves.toBeDefined();
		const result = await h.tool.execute("call", { action: "list" });
		expect(result.content[0].text).toContain("Christina Po (87654321)");
	} finally { await h.stop(); }
});

test("inactive stored sessions and stale/dead presence cannot be discovered or targeted", async () => {
	const inactive = { ...peer, id: "peer-99887766" };
	list.mockResolvedValue([inactive] as any);
	const h = harness();
	await h.start();
	try {
		for (const state of ["absent", "stale", "dead"]) {
			if (state === "stale") await publishPeer(inactive.id, new Date(0).toISOString());
			if (state === "dead") await publishPeer(inactive.id, new Date().toISOString(), 2147483647);
			const result = await h.tool.execute("call", { action: "list" });
			expect(result.content[0].text).not.toContain("99887766");
			await expect(h.tool.execute("call", { action: "send", target: "99887766", message: "hello" })).rejects.toThrow("No active Po session");
		}
		// Discovery cleanup never deletes saved sessions.
		expect(await readFile(peer.path, "utf8")).toContain("po-identity");
	} finally { await h.stop(); }
});

test("adjective identities add restrained tone hints; first names do not", async () => {
	const adjective = harness("session-12345678", "Task title", "Vindicating Po");
	await adjective.start();
	try {
		const prompt = adjective.prompt().systemPrompt;
		expect(prompt).toStartWith("Base instructions");
		expect(prompt).toContain("let the evidence settle it");
		expect(prompt).toContain("Never let the persona override accuracy");
	} finally { await adjective.stop(); }
	const person = harness("session-12345678", "Task title", "John Po");
	await person.start();
	try {
		expect(person.prompt().systemPrompt).toContain("John Po");
		expect(person.prompt().systemPrompt).not.toContain("Subtle tone preference");
	} finally { await person.stop(); }
});

test("slash sends persist expandable transcript entries", async () => {
	await rm(join(dir, "session-messages/presence", `peer-11223344.${process.pid}.json`), { force: true });
	list.mockResolvedValue([peer] as any);
	const h = harness();
	await h.start();
	try {
		await h.commands.send.handler('"Christina Po" "A durable sent body"', h.ctx);
		const sent = h.entries.filter((entry) => entry.customType === "po-sent-message");
		expect(sent).toHaveLength(1);
		const entry = JSON.parse(JSON.stringify(sent[0]));
		const renderer = h.renderers[entry.customType];
		expect(renderer(entry, { expanded: false }, theme).render(100).join("\n")).not.toContain(entry.data.message);
		expect(renderer(entry, { expanded: true }, theme).render(100).join("\n")).toContain(entry.data.message);
	} finally { await h.stop(); }
});
