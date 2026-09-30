import { expect, mock, test } from "bun:test";
import { join } from "node:path";

// Pi supplies typebox at runtime; resolve its global installation for Bun tests.
if (!process.env.NODE_PATH) throw new Error("Set NODE_PATH to Pi's global node_modules directory.");
const typebox = await import(join(process.env.NODE_PATH, "typebox/build/index.mjs"));
mock.module("typebox", () => typebox);
const tui = await import(join(process.env.NODE_PATH, "@earendil-works/pi-tui/dist/index.js"));
mock.module("@earendil-works/pi-tui", () => tui);
const agent = await import(join(process.env.NODE_PATH, "@earendil-works/pi-coding-agent/dist/index.js"));
mock.module("@earendil-works/pi-coding-agent", () => agent);
agent.initTheme("dark", false);
const { AnswerDialog } = await import("./answer-dialog");
const { Questionnaire } = await import("./questionnaire");
const { default: questions } = await import("./index");
const testTheme = { fg: (_: string, text: string) => text, bg: (_: string, text: string) => text, bold: (text: string) => text } as any;

function setup() {
	const handlers = new Map<string, Function>();
	const entries: any[] = [];
	const messages: any[] = [];
	const widgets: any[] = [];
	let tool: any;
	let command: any;
	const ctx: any = {
		mode: "tui",
		sessionManager: { getBranch: () => entries },
		ui: {
			setWidget: (...args: any[]) => widgets.push(args),
			notify() {},
			select: async () => undefined,
			respond: (component: any) => component.handleInput("\x1b"),
			custom: (factory: Function) => new Promise((resolve) => {
				const component = factory({ requestRender() {} }, testTheme, {}, resolve);
				component.focused = true;
				ctx.ui.respond(component);
			}),
		},
	};
	const api: any = {
		on: (name: string, fn: Function) => handlers.set(name, fn),
		registerTool: (value: any) => { tool = value; },
		registerCommand: (_name: string, value: any) => { command = value; },
		appendEntry: (customType: string, data: any) => entries.push({ type: "custom", customType, data }),
		sendUserMessage: (...args: any[]) => messages.push(args),
	};
	questions(api);
	handlers.get("session_start")!({}, ctx);
	return {
		ctx, entries, messages, widgets, handlers, tool,
		ask: (params: any, signal = new AbortController().signal) => tool.execute("call", params, signal, undefined, ctx),
		answer: (args = "") => command.handler(args, ctx),
	};
}

test("non-blocking questions return immediately without opening input", async () => {
	const h = setup();
	h.ctx.ui.select = () => { throw new Error("must not steal focus"); };
	h.ctx.ui.custom = h.ctx.ui.select;
	const result = await h.ask({ question: "Global or project?", options: ["Global", "Project"] });
	expect(result.details.status).toBe("pending");
	expect(h.widgets.at(-1)[1].join("\n")).toContain("Global or project?");
	expect(h.messages).toHaveLength(0);
	expect(h.tool.exposure).toBe("model-only");
});

test("direct answers steer the run and clear the widget", async () => {
	const h = setup();
	const q = await h.ask({ question: "Where?" });
	await h.answer(`${q.details.id} Per project please`);
	expect(h.messages[0][0]).toContain("Per project please");
	expect(h.messages[0][1]).toEqual({ deliverAs: "steer" });
	expect(h.widgets.at(-1)[1]).toBeUndefined();
	await h.answer(`${q.details.id} Duplicate`);
	expect(h.messages).toHaveLength(1);
});

test("choice and custom text paths both deliver the selected answer", async () => {
	const h = setup();
	await h.ask({ question: "Scope?", options: ["Global", "Project"] });
	h.ctx.ui.respond = (c: any) => { c.handleInput("\x1b[B"); c.handleInput("\r"); };
	await h.answer();
	expect(h.messages[0][0]).toEndWith("\nProject");
	await h.ask({ question: "Scope?", options: ["Global"] });
	h.ctx.ui.respond = (c: any) => { c.handleInput("  Something else  "); c.handleInput("\r"); };
	await h.answer();
	expect(h.messages[1][0]).toEndWith("\nSomething else");
});

test("Escape leaves pending; explicit cancellation never grants approval", async () => {
	const h = setup();
	const q = await h.ask({ question: "Deploy?" });
	await h.answer();
	expect(h.messages).toHaveLength(0);
	expect(h.widgets.at(-1)[1]).toBeDefined();
	await h.answer(`${q.details.id} --cancel`);
	expect(h.messages[0][0]).toContain("No answer or approval");
	expect(h.widgets.at(-1)[1]).toBeUndefined();
});

test("blocking questions wait and return answers through the tool only", async () => {
	const h = setup();
	let resolve!: (value: string) => void;
	h.ctx.ui.respond = (c: any) => { resolve = (value: string) => { c.handleInput(value); c.handleInput("\r"); }; };
	let finished = false;
	const run = h.ask({ question: "Proceed?", blocking: true }).then((r: any) => { finished = true; return r; });
	await Promise.resolve();
	expect(finished).toBe(false);
	resolve("Yes");
	expect((await run).details.answer).toBe("Yes");
	expect(h.messages).toHaveLength(0);
	expect(h.entries).toHaveLength(0);
});

test("blocking abort dismisses the dialog and is not approval", async () => {
	const h = setup();
	h.ctx.ui.respond = () => {};
	const controller = new AbortController();
	const run = h.ask({ question: "Proceed?", blocking: true }, controller.signal);
	controller.abort();
	expect((await run).details.status).toBe("cancelled");
});

test("restores only pending questions on the active branch", async () => {
	const h = setup();
	const first = await h.ask({ question: "First?" });
	await h.ask({ question: "Second?" });
	await h.answer(`${first.details.id} Done`);
	h.handlers.get("session_start")!({}, h.ctx);
	const widget = h.widgets.at(-1)[1].join("\n");
	expect(widget).toContain("Second?");
	expect(widget).not.toContain("First?");
	h.entries.length = 0;
	h.handlers.get("session_tree")!({}, h.ctx);
	expect(h.widgets.at(-1)[1]).toBeUndefined();
});

test("session changes invalidate an in-flight answer", async () => {
	const h = setup();
	await h.ask({ question: "Old session?" });
	let resolve!: (value: string) => void;
	h.ctx.ui.respond = (c: any) => { resolve = (value: string) => { c.handleInput(value); c.handleInput("\r"); }; };
	const answering = h.answer();
	h.handlers.get("session_shutdown")!({}, h.ctx);
	h.entries.length = 0;
	h.handlers.get("session_start")!({}, h.ctx);
	resolve("Stale answer");
	await answering;
	expect(h.messages).toHaveLength(0);
});

test("multiple questions open tabs and send exactly one message on final submission", async () => {
	const h = setup();
	await h.ask({ question: "First?", options: ["Yes", "No"] });
	await h.ask({ question: "Second?" });
	h.ctx.ui.respond = (c: any) => {
		expect(c.render(80).join("\n")).toContain("Q2");
		c.handleInput("\r"); // First choice; advance to Q2.
		expect(h.messages).toHaveLength(0);
		c.handleInput("Second answer");
		c.handleInput("\r"); // Advance to Submit, but do not send yet.
		expect(h.messages).toHaveLength(0);
		expect(c.render(80).join("\n")).toContain("Ready to submit");
		c.handleInput("\r");
	};
	await h.answer();
	expect(h.messages).toHaveLength(1);
	expect(h.messages[0][0]).toContain("First?\nAnswer: Yes");
	expect(h.messages[0][0]).toContain("Second?\nAnswer: Second answer");
	expect(h.widgets.at(-1)[1]).toBeUndefined();
});

test("explicit question ID opens that tab; direct text still answers only that question", async () => {
	const h = setup();
	await h.ask({ question: "First?" });
	const second = await h.ask({ question: "Second?" });
	h.ctx.ui.respond = (c: any) => {
		expect(c.render(80).join("\n")).toContain("Second?");
		c.handleInput("\x1b");
	};
	await h.answer(second.details.id);
	expect(h.messages).toHaveLength(0);
	await h.answer(`${second.details.id} Just this one`);
	expect(h.messages).toHaveLength(1);
	expect(h.widgets.at(-1)[1].join("\n")).toContain("First?");
});

test("questions added during a questionnaire remain pending after its submission", async () => {
	const h = setup();
	await h.ask({ question: "First?" });
	await h.ask({ question: "Second?" });
	let component: any;
	h.ctx.ui.respond = (c: any) => { component = c; };
	const answering = h.answer();
	await h.ask({ question: "New question?" });
	for (const input of ["one", "\r", "two", "\r", "\r"]) component.handleInput(input);
	await answering;
	expect(h.messages).toHaveLength(1);
	expect(h.messages[0][0]).not.toContain("New question?");
	expect(h.widgets.at(-1)[1].join("\n")).toContain("New question?");
});

test("group cancellation sends nothing and leaves all questions pending", async () => {
	const h = setup();
	await h.ask({ question: "First?" });
	await h.ask({ question: "Second?" });
	h.ctx.ui.respond = (c: any) => {
		c.handleInput("a draft");
		c.handleInput("\r");
		c.handleInput("\x1b");
	};
	await h.answer();
	expect(h.messages).toHaveLength(0);
	expect(h.widgets.at(-1)[1].join("\n")).toContain("2 pending");
});

test("typing on the custom-answer row keeps the first character without Enter", () => {
	const answers: unknown[] = [];
	const dialog = new AnswerDialog("Scope?", ["Global"], testTheme, () => {}, (answer) => answers.push(answer));
	dialog.focused = true;
	dialog.handleInput("\x1b[B");
	dialog.handleInput("h");
	dialog.handleInput("ello");
	expect(answers).toHaveLength(0);
	expect(dialog.render(80).join("\n")).toContain("hello");
	dialog.handleInput("\r");
	expect(answers).toEqual(["hello"]);
});

test("choices stay visible while the last row is edited inline", () => {
	const dialog = new AnswerDialog("Scope?", ["Global", "Project"], testTheme, () => {}, () => {});
	dialog.focused = true;
	dialog.handleInput("hello");
	for (const width of [24, 80]) {
		const lines = dialog.render(width);
		const rendered = lines.join("\n");
		expect(rendered).toContain("1. Global");
		expect(rendered).toContain("2. Project");
		expect(rendered).toContain("→ 3. hello");
		expect(lines[0]).toBe(new agent.DynamicBorder().render(width)[0]);
		for (const line of lines) expect(tui.visibleWidth(line)).toBeLessThanOrEqual(width);
	}
});

test("arrow navigation preserves the draft and cursor, then submits the right row", () => {
	const answers: unknown[] = [];
	const dialog = new AnswerDialog("Scope?", ["Global", "Project"], testTheme, () => {}, (value) => answers.push(value));
	dialog.focused = true;
	dialog.handleInput("draft");
	dialog.handleInput("\x1b[A");
	expect(dialog.render(80).join("\n")).toContain("→ 2. Project");
	expect(dialog.render(80).join("\n")).toContain("3. draft");
	dialog.handleInput("\x1b[B");
	dialog.handleInput("!");
	dialog.handleInput("\r");
	expect(answers).toEqual(["draft!"]);
	const choice = new AnswerDialog("Scope?", ["Global", "Project"], testTheme, () => {}, (value) => answers.push(value));
	choice.handleInput("unused draft");
	choice.handleInput("\x1b[A");
	choice.handleInput("\r");
	expect(answers).toEqual(["draft!", "Project"]);
});

test("inline input renders within narrow widths with Unicode", () => {
	const dialog = new AnswerDialog("Scope?", ["Global"], testTheme, () => {}, () => {});
	dialog.focused = true;
	dialog.handleInput("🙂é a long answer");
	for (const width of [1, 4, 8, 20]) {
		for (const line of dialog.render(width)) expect(tui.visibleWidth(line)).toBeLessThanOrEqual(width);
	}
});

test("typing Unicode or bracketed paste from any choice begins free text", () => {
	for (const chunks of [["é", "🙂"], ["\x1b[200~", "pasted answer", "\x1b[201~"]]) {
		const answers: unknown[] = [];
		const dialog = new AnswerDialog("Scope?", ["Global"], testTheme, () => {}, (answer) => answers.push(answer));
		for (const chunk of chunks) dialog.handleInput(chunk);
		dialog.handleInput("\r");
		expect(answers).toEqual([chunks.length === 2 ? "é🙂" : "pasted answer"]);
	}
});

test("Enter can still select custom input; blank input does not submit", () => {
	const answers: unknown[] = [];
	const dialog = new AnswerDialog("Scope?", ["Global"], testTheme, () => {}, (answer) => answers.push(answer));
	dialog.handleInput("\x1b[B");
	dialog.handleInput("\r");
	dialog.handleInput("\r");
	expect(answers).toHaveLength(0);
	dialog.handleInput("\x1b");
	expect(answers).toEqual([undefined]);
});

test("questionnaire preserves independent drafts with left/right even while typing", () => {
	const results: unknown[] = [];
	const dialog = new Questionnaire([
		{ id: "a", question: "First?", options: ["One"] },
		{ id: "b", question: "Second?", options: [] },
	], testTheme, () => {}, (result) => results.push(result));
	dialog.focused = true;
	dialog.handleInput("draft one");
	dialog.handleInput("\x1b[C");
	dialog.handleInput("draft two");
	dialog.handleInput("\x1b[D");
	expect(dialog.render(80).join("\n")).toContain("draft one");
	expect(dialog.render(80).join("\n")).toContain("1. One");
	dialog.handleInput("!");
	dialog.handleInput("\x1b[C");
	expect(dialog.render(80).join("\n")).toContain("draft two");
	dialog.handleInput("\x1b[C");
	expect(results).toHaveLength(0);
	dialog.handleInput("\r");
	expect(results).toEqual([[{ id: "a", answer: "draft one!" }, { id: "b", answer: "draft two" }]]);
});

test("Submit requires every answer and editing can invalidate a previous answer", () => {
	const results: unknown[] = [];
	const dialog = new Questionnaire([
		{ id: "a", question: "First?", options: ["Yes"] },
		{ id: "b", question: "Second?", options: ["No"] },
	], testTheme, () => {}, (result) => results.push(result));
	for (const input of ["\x1b[C", "\x1b[C", "\r"]) dialog.handleInput(input);
	expect(results).toHaveLength(0);
	expect(dialog.render(80).join("\n")).toContain("Submit unavailable");
	for (const input of ["\x1b[D", "\x1b[D", "\r", "\r"]) dialog.handleInput(input);
	expect(dialog.render(80).join("\n")).toContain("Ready to submit");
	// Change the second question's selected answer to the blank custom row.
	for (const input of ["\x1b[D", "\x1b[B", "\x1b[C", "\r"]) dialog.handleInput(input);
	expect(results).toHaveLength(0);
	expect(dialog.render(80).join("\n")).toContain("Submit unavailable");
	for (const input of ["\x1b[D", "replacement", "\r", "\r"]) dialog.handleInput(input);
	expect(results).toEqual([[{ id: "a", answer: "Yes" }, { id: "b", answer: "replacement" }]]);
});

test("questionnaire tab bar and summary fit narrow widths", () => {
	const dialog = new Questionnaire([
		{ id: "a", question: "First?", options: [] },
		{ id: "b", question: "Second?", options: [] },
	], testTheme, () => {}, () => {});
	for (const input of ["🙂", "\r", "hello", "\r"]) dialog.handleInput(input);
	for (const width of [1, 8, 24, 80]) {
		for (const line of dialog.render(width)) expect(tui.visibleWidth(line)).toBeLessThanOrEqual(width);
	}
});

test("session changes abort grouped answers without delivering stale drafts", async () => {
	const h = setup();
	await h.ask({ question: "First?" });
	await h.ask({ question: "Second?" });
	let component: any;
	h.ctx.ui.respond = (c: any) => { component = c; };
	const answering = h.answer();
	component.handleInput("old draft");
	h.handlers.get("session_shutdown")!({}, h.ctx);
	h.entries.length = 0;
	h.handlers.get("session_start")!({}, h.ctx);
	await answering;
	expect(h.messages).toHaveLength(0);
});

test("rejects non-TUI use, blank questions, and pre-aborted calls without saving", async () => {
	const h = setup();
	h.ctx.mode = "rpc";
	await expect(h.ask({ question: "Question?" })).rejects.toThrow("interactive terminal");
	h.ctx.mode = "tui";
	await expect(h.ask({ question: "   " })).rejects.toThrow("blank");
	await expect(h.ask({ question: "Question?", options: [" "] })).rejects.toThrow("blank");
	await expect(h.ask({ question: "Question?" }, AbortSignal.abort())).rejects.toThrow("aborted");
	expect(h.entries).toHaveLength(0);
});
