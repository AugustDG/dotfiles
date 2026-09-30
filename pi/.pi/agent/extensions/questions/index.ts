import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { Type } from "typebox";
import { randomUUID } from "node:crypto";
import { showAnswerDialog } from "./answer-dialog";
import { showQuestionnaire, type Question } from "./questionnaire";

const STATE = "po-question-state";
const WIDGET = "po-questions";
type State = { question: Question; status: "pending" | "answered" | "cancelled" };

export default function questions(pi: ExtensionAPI) {
	const pending = new Map<string, Question>();
	let generation = 0;
	let dialog: AbortController | undefined;

	function render(ctx: ExtensionContext) {
		if (ctx.mode !== "tui") return;
		const items = [...pending.values()];
		ctx.ui.setWidget(WIDGET, items.length ? [
			`Po has ${items.length} pending question(s) — /answer to respond`,
			...items.slice(0, 3).map((q) => `▪ ${q.question}`),
			...(items.length > 3 ? [`…and ${items.length - 3} more`] : []),
		] : undefined);
	}

	function save(question: Question, status: State["status"], ctx: ExtensionContext) {
		pi.appendEntry<State>(STATE, { question, status });
		if (status === "pending") pending.set(question.id, question);
		else pending.delete(question.id);
		render(ctx);
	}

	function restore(_event: unknown, ctx: ExtensionContext) {
		generation++;
		dialog?.abort();
		dialog = undefined;
		pending.clear();
		for (const entry of ctx.sessionManager.getBranch()) {
			if (entry.type !== "custom" || entry.customType !== STATE) continue;
			const state = entry.data as State;
			if (!state?.question?.id) continue;
			if (state.status === "pending") pending.set(state.question.id, state.question);
			else pending.delete(state.question.id);
		}
		render(ctx);
	}

	function prompt(q: Question, ctx: ExtensionContext, signal: AbortSignal) {
		return showAnswerDialog(q.question, q.options, ctx, signal);
	}

	pi.registerTool({
		name: "ask_user",
		label: "Ask user",
		exposure: "model-only",
		executionMode: "sequential",
		description: "Ask the user a question with optional choices and free text. By default returns immediately with a pending question ID so you can continue independent work. The user answers with /answer; multiple pending questions are grouped into tabs and submitted together as one user message. Set blocking=true only when no useful work can proceed without an answer. Requires the interactive terminal UI.",
		promptGuidelines: [
			"Use ask_user for clarification or decisions. Prefer non-blocking questions while doing independent work. Never treat a pending, dismissed, or cancelled question as approval, invent an answer, or perform work that depends on an unanswered question. Stop and explain the dependency when independent work is exhausted. Do not post duplicate pending questions.",
		],
		parameters: Type.Object({
			question: Type.String({ minLength: 1, maxLength: 2000 }),
			options: Type.Optional(Type.Array(Type.String({ minLength: 1, maxLength: 500 }), { maxItems: 12 })),
			blocking: Type.Optional(Type.Boolean({ description: "Wait for an answer instead of continuing independent work (default false)" })),
		}),
		async execute(_id, params, signal, _update, ctx) {
			if (ctx.mode !== "tui") throw new Error("ask_user requires the interactive terminal. Ask in ordinary chat instead.");
			if (signal?.aborted) throw new Error("Question aborted.");
			const text = params.question.trim();
			if (!text) throw new Error("Question must not be blank.");
			const q: Question = { id: randomUUID().slice(0, 8), question: text, options: params.options?.map((o) => o.trim()) ?? [] };
			if (q.options.some((o) => !o)) throw new Error("Options must not be blank.");
			if (!params.blocking) {
				save(q, "pending", ctx);
				return {
					content: [{ type: "text", text: `Question ${q.id} pending: ${q.question}\nUser can answer with /answer ${q.id}. Continue only independent work; the answer will arrive as a user message. Pending is not approval.` }],
					details: { ...q, status: "pending" },
				};
			}
			if (dialog) throw new Error("A question dialog is already open. Wait or ask non-blocking instead.");
			const controller = new AbortController();
			dialog = controller;
			const abort = () => controller.abort();
			signal?.addEventListener("abort", abort, { once: true });
			try {
				const answer = await prompt(q, ctx, controller.signal);
				return {
					content: [{ type: "text", text: answer === undefined ? "Question cancelled or dismissed. No answer or approval was given." : `User answered: ${answer}` }],
					details: { ...q, status: answer === undefined ? "cancelled" : "answered", answer },
				};
			} finally {
				signal?.removeEventListener("abort", abort);
				if (dialog === controller) dialog = undefined;
			}
		},
	});

	pi.registerCommand("answer", {
		description: "Answer pending questions in tabs: /answer [id], /answer <id> <text>, or /answer <id> --cancel",
		async handler(args, ctx) {
			if (ctx.mode !== "tui") return;
			if (dialog) { ctx.ui.notify("A question dialog is already open.", "warning"); return; }
			if (!pending.size) { ctx.ui.notify("No pending questions.", "info"); return; }
			const currentGeneration = generation;
			const controller = new AbortController();
			dialog = controller;
			try {
				const match = args.trim().match(/^(\S+)(?:\s+([\s\S]*))?$/);
				const items = [...pending.values()];
				const q = match ? pending.get(match[1]) : items[0];
				if (!q) { ctx.ui.notify("Unknown question ID. Use /answer to open pending questions.", "warning"); return; }
				if (items.length > 1 && !match?.[2]?.trim()) {
					// Snapshot this group. Questions posted while it is open stay pending
					// for the next dialog, rather than changing tabs under the user's cursor.
					const answers = await showQuestionnaire(items, ctx, controller.signal, items.indexOf(q));
					if (!answers || controller.signal.aborted || currentGeneration !== generation || items.some((item) => !pending.has(item.id))) return;
					pi.sendUserMessage(`Answers to your questions:\n\n${answers.map((answer) => {
						const question = items.find((item) => item.id === answer.id)!;
						return `Question ${question.id}: ${question.question}\nAnswer: ${answer.answer}`;
					}).join("\n\n")}`, { deliverAs: "steer" });
					for (const item of items) save(item, "answered", ctx);
					return;
				}
				if (controller.signal.aborted || currentGeneration !== generation) return;
				const cancel = match?.[2]?.trim() === "--cancel";
				const answer = cancel ? undefined : match?.[2]?.trim() || await prompt(q, ctx, controller.signal);
				if (controller.signal.aborted || currentGeneration !== generation || !pending.has(q.id)) return;
				// Escape leaves a non-blocking question pending; cancellation is explicit.
				if (!cancel && answer === undefined) return;
				pi.sendUserMessage(cancel
					? `I cancelled question ${q.id}: ${q.question}\nNo answer or approval is given. Do not proceed with work dependent on this question.`
					: `Answer to question ${q.id}: ${q.question}\n${answer}`,
					{ deliverAs: "steer" });
				save(q, cancel ? "cancelled" : "answered", ctx);
			} finally {
				if (dialog === controller) dialog = undefined;
			}
		},
	});

	pi.on("session_start", restore);
	pi.on("session_tree", restore);
	pi.on("session_shutdown", (_event, ctx) => {
		generation++;
		dialog?.abort();
		dialog = undefined;
		pending.clear();
		if (ctx.mode === "tui") ctx.ui.setWidget(WIDGET, undefined);
	});
}
