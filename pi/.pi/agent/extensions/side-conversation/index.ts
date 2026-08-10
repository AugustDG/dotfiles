import type { AssistantMessage, Message, UserMessage } from "@earendil-works/pi-ai";
import { CustomEditor, type ExtensionAPI, type ExtensionContext, type Theme } from "@earendil-works/pi-coding-agent";
import {
	CURSOR_MARKER,
	Input,
	isFocusable,
	matchesKey,
	truncateToWidth,
	visibleWidth,
	wrapTextWithAnsi,
	type AutocompleteProvider,
	type Component,
	type EditorComponent,
	type Focusable,
	type OverlayHandle,
	type TUI,
} from "@earendil-works/pi-tui";

const STATE_ENTRY = "side-conversation-state";
const MAX_MAIN_CONTEXT_CHARS = 30_000;
const MAX_PERSISTED_TURNS = 50;
const SHORTCUT = "ctrl+shift+s";
const FAKE_CURSOR = /\x1b\[7m([^\x1b]*)\x1b\[(?:0|27)m/g;
const TERMINAL_FOCUS_EVENT = "po:terminal-focus";

function hideFakeCursor(line: string): string {
	return line.replace(FAKE_CURSOR, "$1");
}

interface SideTurn {
	user: string;
	assistant?: AssistantMessage;
	error?: string;
}

interface SideState {
	version?: 2;
	shared: boolean;
	turns: SideTurn[];
}

interface ActivePanel {
	id: symbol;
	panel: SideConversationPanel;
	close: () => void;
}

function emptyState(): SideState {
	return { version: 2, shared: true, turns: [] };
}

function textContent(content: unknown): string {
	if (typeof content === "string") return content;
	if (!Array.isArray(content)) return "";
	return content
		.map((part) => {
			if (!part || typeof part !== "object") return "";
			const value = part as { type?: string; text?: string; name?: string; arguments?: unknown };
			if (value.type === "text" && typeof value.text === "string") return value.text;
			if (value.type === "toolCall") return `[tool call: ${value.name ?? "unknown"}]`;
			return "";
		})
		.filter(Boolean)
		.join("\n");
}

function assistantText(message: AssistantMessage | undefined): string {
	return message ? textContent(message.content) : "";
}

function errorMessage(error: unknown): string {
	return error instanceof Error ? error.message : String(error);
}

function isSideState(value: unknown): value is SideState {
	if (!value || typeof value !== "object") return false;
	const state = value as { shared?: unknown; turns?: unknown };
	return typeof state.shared === "boolean" && Array.isArray(state.turns);
}

function restoreState(ctx: ExtensionContext): SideState {
	let restored = emptyState();
	for (const entry of ctx.sessionManager.getBranch()) {
		if (entry.type !== "custom" || entry.customType !== STATE_ENTRY || !isSideState(entry.data)) continue;
		restored = {
			version: 2,
			// Version 1 defaulted to isolated; migrate existing threads to the new shared default.
			shared: entry.data.version === 2 ? entry.data.shared : true,
			turns: entry.data.turns.slice(-MAX_PERSISTED_TURNS),
		};
	}
	return restored;
}

function formatMainMessage(message: any): string {
	const role = typeof message?.role === "string" ? message.role : "message";
	if (role === "assistant") {
		const text = textContent(message.content);
		return text ? `ASSISTANT:\n${text}` : "";
	}
	if (role === "user") {
		const text = textContent(message.content);
		return text ? `USER:\n${text}` : "";
	}
	if (role === "toolResult") {
		const text = textContent(message.content);
		return `TOOL ${message.toolName ?? "unknown"}:\n${text}`;
	}
	if (role === "bashExecution") return `SHELL:\n${message.command ?? ""}\n${message.output ?? ""}`;
	if (role === "custom" && message.display !== false) {
		const text = textContent(message.content);
		return text ? `CONTEXT:\n${text}` : "";
	}
	if (role === "compactionSummary" || role === "branchSummary") {
		return `SUMMARY:\n${message.summary ?? ""}`;
	}
	return "";
}

function mainContextSnapshot(ctx: ExtensionContext): string {
	const sections: string[] = [];
	for (const entry of ctx.sessionManager.buildContextEntries()) {
		if (entry.type === "message") {
			const formatted = formatMainMessage(entry.message);
			if (formatted) sections.push(formatted);
			continue;
		}
		if (entry.type === "compaction") {
			sections.push(`SUMMARY:\n${entry.summary}`);
			continue;
		}
		if (entry.type === "branch_summary") {
			sections.push(`SUMMARY:\n${entry.summary}`);
			continue;
		}
		if (entry.type === "custom_message" && entry.display !== false) {
			const text = textContent(entry.content);
			if (text) sections.push(`CONTEXT:\n${text}`);
		}
	}
	const transcript = sections.join("\n\n");
	if (transcript.length <= MAX_MAIN_CONTEXT_CHARS) return transcript;
	return `[Earlier main-conversation context omitted]\n\n${transcript.slice(-MAX_MAIN_CONTEXT_CHARS)}`;
}

function sideMessages(turns: SideTurn[], currentUser: string): Message[] {
	const messages: Message[] = [];
	for (const turn of turns) {
		if (!turn.assistant) continue;
		messages.push({
			role: "user",
			content: [{ type: "text", text: turn.user }],
			timestamp: turn.assistant.timestamp - 1,
		});
		messages.push(turn.assistant);
	}
	const current: UserMessage = {
		role: "user",
		content: [{ type: "text", text: currentUser }],
		timestamp: Date.now(),
	};
	messages.push(current);
	return messages;
}

function systemPrompt(ctx: ExtensionContext, shared: boolean): string {
	let prompt = `You are a concise side-conversation assistant running beside a main coding-agent conversation.
Answer the side user's question directly. You have no tools in this side thread, so do not claim to inspect or modify files.
The working directory is ${ctx.cwd}.`;
	if (shared) {
		const snapshot = mainContextSnapshot(ctx);
		prompt += `\n\nThe user explicitly enabled access to this snapshot of the main conversation. Use it as context, but follow the latest side-thread request:\n\n<main_conversation>\n${snapshot || "(main conversation is empty)"}\n</main_conversation>`;
	}
	return prompt;
}

interface CursorAwareEditor extends EditorComponent {
	getCursor?: () => { line: number; col: number };
	getLines?: () => string[];
	isShowingAutocomplete?: () => boolean;
}

interface AppAwareEditor extends EditorComponent {
	actionHandlers?: Map<unknown, () => void>;
	onEscape?: () => void;
	onCtrlD?: () => void;
	onPasteImage?: () => void;
	onExtensionShortcut?: (data: string) => boolean;
	disableSubmit?: boolean;
}

function findCursorAwareEditor(editor: unknown, seen = new Set<unknown>()): CursorAwareEditor | undefined {
	if (!editor || typeof editor !== "object" || seen.has(editor)) return undefined;
	seen.add(editor);
	const candidate = editor as CursorAwareEditor & { base?: unknown };
	if (typeof candidate.getCursor === "function" && typeof candidate.getLines === "function") return candidate;
	// Editor wrappers commonly retain their delegated component in a `base` field.
	return findCursorAwareEditor(candidate.base, seen);
}

class SidebarBoundaryEditor implements EditorComponent, Focusable {
	constructor(
		private readonly base: EditorComponent,
		private readonly focusSidebar: () => boolean,
	) {}

	get focused(): boolean {
		return isFocusable(this.base) ? this.base.focused : false;
	}

	set focused(value: boolean) {
		if (isFocusable(this.base)) this.base.focused = value;
	}

	get actionHandlers(): Map<unknown, () => void> | undefined {
		return (this.base as AppAwareEditor).actionHandlers;
	}

	get onEscape(): (() => void) | undefined {
		return (this.base as AppAwareEditor).onEscape;
	}

	set onEscape(value: (() => void) | undefined) {
		(this.base as AppAwareEditor).onEscape = value;
	}

	get onCtrlD(): (() => void) | undefined {
		return (this.base as AppAwareEditor).onCtrlD;
	}

	set onCtrlD(value: (() => void) | undefined) {
		(this.base as AppAwareEditor).onCtrlD = value;
	}

	get onPasteImage(): (() => void) | undefined {
		return (this.base as AppAwareEditor).onPasteImage;
	}

	set onPasteImage(value: (() => void) | undefined) {
		(this.base as AppAwareEditor).onPasteImage = value;
	}

	get onExtensionShortcut(): ((data: string) => boolean) | undefined {
		return (this.base as AppAwareEditor).onExtensionShortcut;
	}

	set onExtensionShortcut(value: ((data: string) => boolean) | undefined) {
		(this.base as AppAwareEditor).onExtensionShortcut = value;
	}

	get wantsKeyRelease(): boolean | undefined {
		return this.base.wantsKeyRelease;
	}

	get disableSubmit(): boolean | undefined {
		return (this.base as AppAwareEditor).disableSubmit;
	}

	set disableSubmit(value: boolean | undefined) {
		(this.base as AppAwareEditor).disableSubmit = value;
	}

	get onSubmit(): ((text: string) => void) | undefined {
		return this.base.onSubmit;
	}

	set onSubmit(value: ((text: string) => void) | undefined) {
		this.base.onSubmit = value;
	}

	get onChange(): ((text: string) => void) | undefined {
		return this.base.onChange;
	}

	set onChange(value: ((text: string) => void) | undefined) {
		this.base.onChange = value;
	}

	get borderColor(): ((text: string) => string) | undefined {
		return this.base.borderColor;
	}

	set borderColor(value: ((text: string) => string) | undefined) {
		this.base.borderColor = value;
	}

	private isAtEnd(): boolean {
		const editor = findCursorAwareEditor(this.base);
		if (!editor || editor.isShowingAutocomplete?.()) return false;
		const cursor = editor.getCursor?.();
		const lines = editor.getLines?.();
		if (!cursor || !lines || lines.length === 0) return false;
		const lastLine = lines.length - 1;
		return cursor.line === lastLine && cursor.col === (lines[lastLine]?.length ?? 0);
	}

	handleInput(data: string): void {
		if (matchesKey(data, "right") && this.isAtEnd() && this.focusSidebar()) return;
		this.base.handleInput(data);
	}

	render(width: number): string[] {
		return this.base.render(width);
	}

	invalidate(): void {
		this.base.invalidate();
	}

	getText(): string {
		return this.base.getText();
	}

	setText(text: string): void {
		this.base.setText(text);
	}

	addToHistory(text: string): void {
		this.base.addToHistory?.(text);
	}

	insertTextAtCursor(text: string): void {
		if (this.base.insertTextAtCursor) this.base.insertTextAtCursor(text);
		else this.base.setText(this.base.getText() + text);
	}

	getExpandedText(): string {
		return this.base.getExpandedText?.() ?? this.base.getText();
	}

	setAutocompleteProvider(provider: AutocompleteProvider): void {
		this.base.setAutocompleteProvider?.(provider);
	}

	setPaddingX(padding: number): void {
		this.base.setPaddingX?.(padding);
	}

	setAutocompleteMaxVisible(maxVisible: number): void {
		this.base.setAutocompleteMaxVisible?.(maxVisible);
	}

	dispose(): void {
		(this.base as EditorComponent & { dispose?: () => void }).dispose?.();
	}
}

class SideConversationPanel implements Component, Focusable {
	private readonly input = new Input();
	private readonly abortController = { current: undefined as AbortController | undefined };
	private _focused = false;
	private handle?: OverlayHandle;
	private status = "";
	private disposed = false;

	constructor(
		private readonly tui: TUI,
		private readonly theme: Theme,
		private readonly ctx: ExtensionContext,
		private readonly getState: () => SideState,
		private readonly hasTerminalFocus: () => boolean,
		private readonly persist: () => void,
		private readonly onClose: () => void,
	) {
		this.input.onSubmit = (value) => void this.submit(value);
	}

	get focused(): boolean {
		return this._focused;
	}

	set focused(value: boolean) {
		this._focused = value;
		this.input.focused = value;
	}

	setHandle(handle: OverlayHandle): void {
		this.handle = handle;
	}

	requestRender(): void {
		if (!this.disposed) this.tui.requestRender();
	}

	focus(): void {
		this.handle?.focus();
		this.requestRender();
	}

	toggleFocus(): void {
		if (!this.handle) return;
		if (this.handle.isFocused()) this.handle.unfocus();
		else this.handle.focus();
		this.requestRender();
	}

	setShared(shared: boolean): void {
		this.getState().shared = shared;
		this.status = shared ? "Main context enabled" : "Isolated mode enabled";
		this.persist();
		this.requestRender();
	}

	private async submit(raw: string): Promise<void> {
		const prompt = raw.trim();
		if (!prompt || this.abortController.current) return;
		if (!this.ctx.model) {
			this.status = "No model selected";
			this.requestRender();
			return;
		}

		this.input.setValue("");
		const state = this.getState();
		const priorTurns = [...state.turns];
		const turn: SideTurn = { user: prompt };
		state.turns.push(turn);
		if (state.turns.length > MAX_PERSISTED_TURNS) state.turns.splice(0, state.turns.length - MAX_PERSISTED_TURNS);
		this.status = `Thinking with ${this.ctx.model.id}…`;
		this.persist();
		this.requestRender();

		const controller = new AbortController();
		this.abortController.current = controller;
		try {
			const response = await this.ctx.modelRegistry.complete(
				this.ctx.model,
				{
					systemPrompt: systemPrompt(this.ctx, state.shared),
					messages: sideMessages(priorTurns, prompt),
				},
				{ signal: controller.signal },
			);
			if (response.stopReason === "error" || response.stopReason === "aborted") {
				throw new Error(response.errorMessage || `Side response ${response.stopReason}`);
			}
			turn.assistant = response;
			this.status = response.stopReason === "length" ? "Reply reached its length limit" : "";
		} catch (error) {
			turn.error = controller.signal.aborted ? "Request cancelled" : errorMessage(error);
			this.status = turn.error;
		} finally {
			if (this.abortController.current === controller) this.abortController.current = undefined;
			if (!this.disposed) {
				this.persist();
				this.requestRender();
			}
		}
	}

	private inputIsAtStart(): boolean {
		const width = Math.max(10, visibleWidth(this.input.getValue()) + 4);
		const [rendered = ""] = this.input.render(width);
		const markerIndex = rendered.indexOf(CURSOR_MARKER);
		if (markerIndex < 0) return false;
		return visibleWidth(rendered.slice(0, markerIndex)) === 2;
	}

	handleInput(data: string): void {
		if (matchesKey(data, SHORTCUT)) {
			this.toggleFocus();
			return;
		}
		if (matchesKey(data, "left") && this.inputIsAtStart()) {
			this.handle?.unfocus();
			this.requestRender();
			return;
		}
		if (matchesKey(data, "alt+c")) {
			this.setShared(!this.getState().shared);
			return;
		}
		if (matchesKey(data, "ctrl+c") && this.abortController.current) {
			this.abortController.current.abort();
			this.status = "Cancelling…";
			this.requestRender();
			return;
		}
		if (matchesKey(data, "escape") || matchesKey(data, "ctrl+c")) {
			this.onClose();
			return;
		}
		this.input.handleInput(data);
		this.requestRender();
	}

	private padded(content: string, width: number): string {
		const truncated = truncateToWidth(content, width, "…", true);
		return truncated + " ".repeat(Math.max(0, width - visibleWidth(truncated)));
	}

	private wrappedMessage(label: string, text: string, width: number, color: "accent" | "text" | "error"): string[] {
		const prefix = this.theme.fg(color, this.theme.bold(label));
		const clean = text.trim() || "(empty)";
		const paragraphs = clean.split("\n");
		const lines: string[] = [];
		for (let index = 0; index < paragraphs.length; index++) {
			const paragraph = paragraphs[index] || " ";
			const wrapped = wrapTextWithAnsi(paragraph, width);
			if (index === 0) lines.push(prefix, ...wrapped);
			else lines.push(...wrapped);
		}
		return lines;
	}

	render(width: number): string[] {
		const innerWidth = Math.max(10, width - 2);
		const border = (value: string) => this.theme.fg(this.focused ? "borderAccent" : "border", value);
		const state = this.getState();
		const mode = state.shared ? this.theme.fg("success", "MAIN CONTEXT") : this.theme.fg("muted", "ISOLATED");
		const title = ` Side conversation · ${mode} `;
		const titleWidth = visibleWidth(title);
		const topFill = Math.max(0, innerWidth - titleWidth);
		const lines: string[] = [border("╭") + title + border(`${"─".repeat(topFill)}╮`)];

		const transcript: string[] = [];
		if (state.turns.length === 0) {
			transcript.push(this.theme.fg("dim", "Ask a quick question here without interrupting the main thread."));
		}
		for (const turn of state.turns) {
			transcript.push(...this.wrappedMessage("You", turn.user, innerWidth, "accent"));
			const reply = assistantText(turn.assistant);
			if (reply) transcript.push(...this.wrappedMessage("Side", reply, innerWidth, "text"));
			else if (turn.error) transcript.push(...this.wrappedMessage("Error", turn.error, innerWidth, "error"));
			else transcript.push(this.theme.fg("warning", this.theme.bold("Side")), this.theme.fg("warning", "Thinking…"));
			transcript.push("");
		}

		// Seven rows are used by the header, separator, input, status/help, and footer.
		// Fill every remaining terminal row so the overlay behaves like a full-height sidebar.
		const transcriptHeight = Math.max(1, this.tui.terminal.rows - 7);
		const visibleTranscript = transcript.slice(-transcriptHeight);
		for (let index = visibleTranscript.length; index < transcriptHeight; index++) {
			lines.push(border("│") + " ".repeat(innerWidth) + border("│"));
		}
		for (const line of visibleTranscript) lines.push(border("│") + this.padded(line, innerWidth) + border("│"));

		lines.push(border("├") + border("─".repeat(innerWidth)) + border("┤"));
		const [renderedInput = ""] = this.input.render(innerWidth);
		const inputLine = this.focused && this.hasTerminalFocus() ? renderedInput : hideFakeCursor(renderedInput);
		lines.push(border("│") + this.padded(inputLine, innerWidth) + border("│"));
		const status = this.status ? ` ${this.status}` : "";
		lines.push(border("│") + this.padded(this.theme.fg("dim", status), innerWidth) + border("│"));
		lines.push(
			border("│") +
				this.padded(this.theme.fg("dim", " Enter send · ← at start: main · Alt+C context"), innerWidth) +
				border("│"),
		);
		lines.push(
			border("│") +
				this.padded(this.theme.fg("dim", " Esc close"), innerWidth) +
				border("│"),
		);
		lines.push(border(`╰${"─".repeat(innerWidth)}╯`));
		return lines;
	}

	invalidate(): void {
		this.input.invalidate();
	}

	dispose(): void {
		this.disposed = true;
		this.abortController.current?.abort();
		this.abortController.current = undefined;
	}
}

export default function sideConversationExtension(pi: ExtensionAPI) {
	let state = emptyState();
	let active: ActivePanel | undefined;
	let restoreEditor: (() => void) | undefined;
	let editorInstalled = false;
	let terminalFocused = true;

	pi.events.on(TERMINAL_FOCUS_EVENT, (focused) => {
		if (typeof focused !== "boolean" || focused === terminalFocused) return;
		terminalFocused = focused;
		active?.panel.requestRender();
	});

	const persist = () => {
		pi.appendEntry(STATE_ENTRY, {
			version: 2,
			shared: state.shared,
			turns: state.turns.slice(-MAX_PERSISTED_TURNS),
		} satisfies SideState);
	};

	const closePanel = () => {
		const panel = active;
		if (!panel) return;
		panel.close();
	};

	const installBoundaryEditor = (ctx: ExtensionContext) => {
		if (editorInstalled || ctx.mode !== "tui") return;
		const previousEditor = ctx.ui.getEditorComponent();
		ctx.ui.setEditorComponent((tui, theme, keybindings) => {
			const base = previousEditor?.(tui, theme, keybindings) ?? new CustomEditor(tui, theme, keybindings);
			return new SidebarBoundaryEditor(base, () => {
				if (!active) return false;
				active.panel.focus();
				return true;
			});
		});
		restoreEditor = () => ctx.ui.setEditorComponent(previousEditor);
		editorInstalled = true;
	};

	const openPanel = (ctx: ExtensionContext) => {
		if (ctx.mode !== "tui") {
			ctx.ui.notify("Side conversation requires interactive mode", "error");
			return;
		}
		if (active) {
			active.panel.focus();
			return;
		}
		installBoundaryEditor(ctx);

		const id = Symbol("side-panel");
		let doneOverlay: (() => void) | undefined;
		let panel: SideConversationPanel | undefined;
		const promise = ctx.ui.custom<void>(
			(tui, theme, _keybindings, done) => {
				doneOverlay = () => done();
				panel = new SideConversationPanel(
					tui,
					theme,
					ctx,
					() => state,
					() => terminalFocused,
					persist,
					() => done(),
				);
				return panel;
			},
			{
				overlay: true,
				overlayOptions: {
					anchor: "top-right",
					width: "42%",
					minWidth: 46,
					maxHeight: "100%",
					margin: 0,
				},
				onHandle: (handle) => panel?.setHandle(handle),
			},
		);

		if (!panel || !doneOverlay) {
			ctx.ui.notify("Could not open side conversation", "error");
			return;
		}
		active = { id, panel, close: doneOverlay };
		void promise
			.catch((error) => ctx.ui.notify(`Side conversation failed: ${errorMessage(error)}`, "error"))
			.finally(() => {
				if (active?.id === id) active = undefined;
			});
	};

	pi.on("session_start", (_event, ctx) => {
		state = restoreState(ctx);
	});

	pi.on("session_shutdown", () => {
		closePanel();
		active = undefined;
		restoreEditor?.();
		restoreEditor = undefined;
		editorInstalled = false;
	});

	pi.registerCommand("side", {
		description: "Open or manage a side conversation: /side [shared|isolated|clear|close]",
		async handler(args, ctx) {
			const action = args.trim().toLowerCase();
			if (!action) {
				openPanel(ctx);
				return;
			}
			if (action === "close") {
				closePanel();
				return;
			}
			if (action === "clear") {
				state.turns = [];
				persist();
				active?.panel.requestRender();
				ctx.ui.notify("Side conversation cleared", "info");
				return;
			}
			if (action === "shared" || action === "isolated") {
				const shared = action === "shared";
				state.shared = shared;
				persist();
				active?.panel.requestRender();
				ctx.ui.notify(`Side conversation: ${shared ? "main context enabled" : "isolated"}`, "info");
				return;
			}
			ctx.ui.notify("Usage: /side [shared|isolated|clear|close]", "warning");
		},
	});

	pi.registerShortcut(SHORTCUT, {
		description: "Open or focus the side conversation",
		handler(ctx) {
			if (!active) openPanel(ctx);
			else active.panel.toggleFocus();
		},
	});
}
