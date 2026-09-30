import { DynamicBorder, type ExtensionContext, type Theme } from "@earendil-works/pi-coding-agent";
import { getKeybindings, Key, matchesKey, Text, truncateToWidth } from "@earendil-works/pi-tui";
import { AnswerDialog, showDialog } from "./answer-dialog";

export type Question = { id: string; question: string; options: string[] };
export type Answer = { id: string; answer: string };
const HELP = "←/→ questions · ↑↓ choices · Just type to answer · Enter next · Esc cancel";

/** One draft per question; nothing is delivered until the final Submit tab. */
export class Questionnaire {
	private readonly dialogs: AnswerDialog[];
	private readonly border = new DynamicBorder();
	private current: number;
	private hasFocus = false;

	constructor(
		private readonly questions: Question[],
		private readonly theme: Theme,
		private readonly refresh: () => void,
		private readonly done: (answers: Answer[] | undefined) => void,
		initialIndex = 0,
	) {
		if (!questions.length) throw new Error("A questionnaire needs at least one question.");
		this.current = Math.max(0, Math.min(initialIndex, questions.length - 1));
		this.dialogs = questions.map((q, index) => new AnswerDialog(q.question, q.options, theme, refresh, (answer) => {
			if (answer === undefined) this.done(undefined);
			else this.moveTo(index + 1);
		}));
	}

	get focused() { return this.hasFocus; }
	set focused(value: boolean) {
		this.hasFocus = value;
		this.dialogs.forEach((dialog, index) => { dialog.focused = value && index === this.current; });
	}

	private moveTo(index: number) {
		this.current = Math.max(0, Math.min(index, this.questions.length));
		this.focused = this.hasFocus;
		this.refresh();
	}

	private allAnswered() { return this.dialogs.every((dialog) => dialog.getAnswer() !== undefined); }

	handleInput(data: string) {
		const kb = getKeybindings();
		if (kb.matches(data, "tui.select.cancel")) {
			this.done(undefined);
		} else if (matchesKey(data, Key.left) || matchesKey(data, Key.shift("tab"))) {
			this.moveTo(this.current - 1);
		} else if (matchesKey(data, Key.right) || matchesKey(data, Key.tab)) {
			this.moveTo(this.current + 1);
		} else if (this.current < this.questions.length) {
			this.dialogs[this.current].handleInput(data);
		} else if ((kb.matches(data, "tui.select.confirm") || data === "\n") && this.allAnswered()) {
			this.done(this.questions.map((q, index) => ({ id: q.id, answer: this.dialogs[index].getAnswer()! })));
		}
	}

	render(width: number): string[] {
		const columns = Math.max(1, width);
		const text = (value: string) => new Text(value, 1, 0).render(columns).map((line) => truncateToWidth(line, columns, ""));
		const ready = this.allAnswered();
		const labels = this.questions.map((_q, i) => `${this.dialogs[i].getAnswer() !== undefined ? "✓" : "○"} Q${i + 1}`);
		labels.push(ready ? "✓ Submit" : "Submit");
		const tabs = labels.map((label, i) => {
			const value = ` ${label} `;
			if (i === this.current) return this.theme.bg("selectedBg", this.theme.fg("text", value));
			return this.theme.fg(i === this.questions.length && !ready ? "dim" : "muted", value);
		}).join(" ");
		const tabLines = text(tabs);
		if (this.current < this.questions.length) {
			const content = this.dialogs[this.current].render(columns, HELP);
			return [content[0], "", ...tabLines, ...content.slice(1)];
		}
		return [
			...this.border.render(columns), "", ...tabLines, "",
			...text(this.theme.fg("accent", this.theme.bold(ready ? "Ready to submit" : "Answer every question to submit"))), "",
			...this.questions.flatMap((q, i) => text(`Q${i + 1}: ${q.question}\n${this.dialogs[i].getAnswer() ?? "(unanswered)"}`)), "",
			...text(this.theme.fg(ready ? "success" : "dim", ready ? "Enter to submit all answers" : "Submit unavailable — use ←/→ to finish answering")),
			...text(this.theme.fg("dim", "←/→ questions · Esc cancel")), "", ...this.border.render(columns),
		];
	}

	invalidate() {
		this.border.invalidate();
		this.dialogs.forEach((dialog) => dialog.invalidate());
	}

	dispose() { this.dialogs.forEach((dialog) => dialog.dispose()); }
}

export function showQuestionnaire(questions: Question[], ctx: ExtensionContext, signal: AbortSignal, initialIndex = 0) {
	return showDialog<Answer[]>(ctx, signal, (theme, refresh, done) => new Questionnaire(questions, theme, refresh, done, initialIndex));
}
