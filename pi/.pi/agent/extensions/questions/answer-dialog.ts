import {
	DynamicBorder,
	type ExtensionContext,
	type Theme,
} from "@earendil-works/pi-coding-agent";
import { getKeybindings, Input, Text, visibleWidth, truncateToWidth, type Component, type Focusable } from "@earendil-works/pi-tui";

/** Native-style choices with a persistent, editable final answer row. */
export class AnswerDialog {
	private readonly input = new Input({ prompt: "", placeholder: "Type an answer…" });
	private readonly border = new DynamicBorder();
	private selected = 0;
	private hasFocus = false;
	private confirmed = false;

	constructor(
		private readonly question: string,
		private readonly options: string[],
		private readonly theme: Theme,
		private readonly refresh: () => void,
		private readonly done: (answer: string | undefined) => void,
	) {}

	getAnswer(): string | undefined {
		if (this.selected === this.options.length) return this.input.getValue().trim() || undefined;
		return this.confirmed ? this.options[this.selected] : undefined;
	}

	get focused() { return this.hasFocus; }
	set focused(value: boolean) {
		this.hasFocus = value;
		this.input.focused = value && this.selected === this.options.length;
	}

	handleInput(data: string) {
		const kb = getKeybindings();
		if (kb.matches(data, "tui.select.cancel")) {
			this.done(undefined);
			return;
		}
		if (kb.matches(data, "tui.select.up")) {
			const next = Math.max(0, this.selected - 1);
			if (next !== this.selected) this.confirmed = false;
			this.selected = next;
		} else if (kb.matches(data, "tui.select.down")) {
			const next = Math.min(this.options.length, this.selected + 1);
			if (next !== this.selected) this.confirmed = false;
			this.selected = next;
		} else if (kb.matches(data, "tui.select.confirm") || data === "\n") {
			this.confirmed = true;
			const answer = this.getAnswer();
			if (answer !== undefined) this.done(answer);
		} else {
			// Decode text/paste with Input itself. Keep one input instance so drafts,
			// cursor position, and undo history survive navigation between choices.
			const before = this.input.getValue();
			this.input.handleInput(data);
			if (this.input.getValue() !== before) this.selected = this.options.length;
		}
		this.focused = this.hasFocus;
		this.refresh();
	}

	invalidate() {
		this.input.invalidate();
		this.border.invalidate();
	}

	render(width: number, help = "↑↓ navigate · Just type to answer · Enter submit · Esc cancel"): string[] {
		const columns = Math.max(1, width);
		const text = (value: string) => new Text(value, 1, 0).render(columns).map((line) => truncateToWidth(line, columns, ""));
		const lines = [
			...this.border.render(columns), "",
			...text(this.theme.fg("accent", this.theme.bold(this.question))), "",
		];
		for (let i = 0; i <= this.options.length; i++) {
			const selected = i === this.selected;
			const prefix = `${selected ? "→" : " "} ${i + 1}. `;
			if (i === this.options.length && selected) {
				const styledPrefix = this.theme.fg("accent", ` ${prefix}`);
				const remaining = columns - visibleWidth(styledPrefix);
				if (remaining > 0) {
					lines.push(...this.input.render(remaining).map((line) => styledPrefix + line));
				} else {
					lines.push(...text(prefix), ...this.input.render(columns));
				}
			} else {
				const label = i < this.options.length ? this.options[i] : this.input.getValue() || "Type an answer…";
				lines.push(...text(this.theme.fg(selected ? "accent" : "text", prefix + label)));
			}
		}
		lines.push("", ...text(this.theme.fg("dim", help)), "", ...this.border.render(columns));
		return lines;
	}

	dispose() {}
}

export function showAnswerDialog(question: string, options: string[], ctx: ExtensionContext, signal: AbortSignal) {
	return showDialog<string>(ctx, signal, (theme, refresh, done) => new AnswerDialog(question, options, theme, refresh, done));
}

export async function showDialog<T>(
	ctx: ExtensionContext,
	signal: AbortSignal,
	build: (theme: Theme, refresh: () => void, done: (answer: T | undefined) => void) => Component & Focusable & { dispose?(): void },
): Promise<T | undefined> {
	if (signal.aborted) return undefined;
	let finish: ((answer: T | undefined) => void) | undefined;
	// Defer dismissal until the component factory has returned.
	const abort = () => queueMicrotask(() => finish?.(undefined));
	signal.addEventListener("abort", abort, { once: true });
	try {
		return await ctx.ui.custom<T | undefined>((tui, theme, _keys, done) => {
			let settled = false;
			finish = (answer) => {
				if (settled) return;
				settled = true;
				done(signal.aborted ? undefined : answer);
			};
			if (signal.aborted) abort();
			return build(theme, () => tui.requestRender(), finish);
		});
	} finally {
		signal.removeEventListener("abort", abort);
		finish = undefined;
	}
}
