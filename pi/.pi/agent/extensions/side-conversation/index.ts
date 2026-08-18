import {
	validateToolCall,
	type AssistantMessage,
	type ImageContent,
	type Message,
	type TextContent,
	type ToolCall,
	type ToolResultMessage,
	type UserMessage,
} from "@earendil-works/pi-ai";
import {
	createCodingTools,
	createReadTool,
	CustomEditor,
	type ExtensionAPI,
	type ExtensionContext,
	type KeybindingsManager,
	type Theme,
} from "@earendil-works/pi-coding-agent";
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
const MAX_TOOL_ROUNDS = 12;
const MAX_TOOL_CALLS = 24;
const MAX_TOOL_DETAIL_CHARS = 180;
const MAX_TOOL_COMMAND_CHARS = 120;
const SHORTCUT = "ctrl+shift+s";
const FAKE_CURSOR = /\x1b\[7m([^\x1b]*)\x1b\[(?:0|27)m/g;
const TERMINAL_FOCUS_EVENT = "po:terminal-focus";
const IMAGE_PATH = /\.(?:png|jpe?g|gif|webp|bmp)$/i;

function hideFakeCursor(line: string): string {
	return line.replace(FAKE_CURSOR, "$1");
}

interface SideToolRun {
	name: string;
	input: string;
	output?: string;
	isError?: boolean;
}

interface SideTurn {
	user: string;
	imagePaths?: string[];
	assistant?: AssistantMessage;
	tools?: SideToolRun[];
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

interface WheelEvent {
	direction: -1 | 1;
	x: number;
	y: number;
}

interface WheelRoutableTui extends TUI {
	routeWheel?: (event: WheelEvent) => void;
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

function systemPrompt(ctx: ExtensionContext, shared: boolean): string {
	let prompt = `You are a concise side-conversation coding assistant running beside a main coding-agent conversation.
You can use read, bash, edit, and write tools in the working directory ${ctx.cwd}.

The side thread and main session share the same working tree and may run concurrently. Avoid conflicting with the main session whenever possible:
- Prefer read-only inspection unless the user asks you to make or verify a change.
- Before editing or writing, inspect the current file and make the smallest targeted change.
- Avoid files the main-session snapshot indicates are actively being changed; if overlap is likely, explain the risk and ask the user to coordinate instead.
- Do not switch branches or run git reset, checkout, restore, clean, stash, mass-formatting, dependency installation, or other broad workspace-changing commands unless explicitly requested.
- Do not overwrite unexpected changes. Re-read a target if there may have been concurrent edits.
- Run independent commands narrowly and report every file you modify.

Answer the side user's request directly and never claim to have inspected or modified something unless a tool result confirms it.`;
	if (shared) {
		const snapshot = mainContextSnapshot(ctx);
		prompt += `\n\nThe user explicitly enabled access to this snapshot of the main conversation. Use it as context, but follow the latest side-thread request:\n\n<main_conversation>\n${snapshot || "(main conversation is empty)"}\n</main_conversation>`;
	}
	return prompt;
}

function compactWhitespace(text: string): string {
	return text.replace(/\s+/g, " ").trim();
}

function truncateToolDetail(text: string, limit = MAX_TOOL_DETAIL_CHARS): string {
	const compact = compactWhitespace(text);
	return compact.length <= limit ? compact : `${compact.slice(0, Math.max(0, limit - 1))}…`;
}

function compactToolPath(path: string, cwd: string): string {
	const normalizedCwd = cwd.replace(/\/+$/, "");
	if (path === normalizedCwd) return ".";
	if (path.startsWith(`${normalizedCwd}/`)) return path.slice(normalizedCwd.length + 1);
	return path;
}

function toolCallDisplay(call: ToolCall, cwd: string): string {
	const args = call.arguments ?? {};
	if (call.name === "bash" && typeof args.command === "string") {
		return truncateToolDetail(args.command, MAX_TOOL_COMMAND_CHARS);
	}
	if (typeof args.path === "string") {
		const path = compactToolPath(args.path, cwd);
		if (call.name === "read") {
			const offset = typeof args.offset === "number" ? args.offset : 1;
			if (typeof args.limit === "number") return `${path}:${offset}–${offset + args.limit - 1}`;
			if (typeof args.offset === "number") return `${path}:${offset}+`;
		}
		if (call.name === "edit" && Array.isArray(args.edits)) {
			return `${path} · ${args.edits.length} ${args.edits.length === 1 ? "change" : "changes"}`;
		}
		if (call.name === "write" && typeof args.content === "string") {
			return `${path} · ${args.content.split("\n").length} lines`;
		}
		return path;
	}
	return truncateToolDetail(JSON.stringify(args));
}

function visibleResultLines(text: string): number {
	if (!text) return 0;
	return text.split("\n").filter((line) => !/^\[\d+ more lines in file\./.test(line.trim())).length;
}

function diffStats(diff: string): string {
	let additions = 0;
	let removals = 0;
	for (const line of diff.split("\n")) {
		if (line.startsWith("+") && !line.startsWith("+++")) additions++;
		if (line.startsWith("-") && !line.startsWith("---")) removals++;
	}
	return `+${additions} −${removals}`;
}

function toolResultDisplay(message: ToolResultMessage, call: ToolCall): string {
	const text = textContent(message.content).trim();
	if (message.isError) return truncateToolDetail(text || "failed");

	if (call.name === "read") {
		if (message.content.some((part) => part.type === "image")) return "image loaded";
		const lineCount = visibleResultLines(text);
		return `${lineCount} ${lineCount === 1 ? "line" : "lines"}`;
	}
	if (call.name === "bash") {
		const lineCount = text ? text.split("\n").length : 0;
		return lineCount > 0 ? `${lineCount} output ${lineCount === 1 ? "line" : "lines"}` : "done";
	}
	if (call.name === "edit") {
		const diff = (message.details as { diff?: unknown } | undefined)?.diff;
		return typeof diff === "string" ? diffStats(diff) : "applied";
	}
	if (call.name === "write") return "written";
	return truncateToolDetail(text || "done");
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
		private readonly pasteIntoSidebar: (content: string) => boolean,
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
		if (this.pasteIntoSidebar(text)) return;
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
	private readonly imageCache = new Map<string, ImageContent>();
	private readonly pendingImagePaths: string[] = [];
	private clearOnSettle = false;
	private _focused = false;
	private handle?: OverlayHandle;
	private status = "";
	private scrollTop?: number;
	private transcriptLineCount = 0;
	private transcriptHeight = 1;
	private panelWidth = 0;
	private restoreWheelRoute?: () => void;
	private disposed = false;

	constructor(
		private readonly tui: TUI,
		private readonly theme: Theme,
		private readonly keybindings: KeybindingsManager,
		private readonly ctx: ExtensionContext,
		private readonly getState: () => SideState,
		private readonly hasTerminalFocus: () => boolean,
		private readonly requestClipboardPaste: () => void,
		private readonly persist: () => void,
		private readonly onClose: () => void,
	) {
		this.input.onSubmit = (value) => void this.submit(value);
		this.installWheelRoute();
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

	clearConversation(): void {
		if (this.abortController.current) {
			this.clearOnSettle = true;
			this.abortController.current.abort();
			this.status = "Clearing…";
			this.requestRender();
			return;
		}
		this.getState().turns = [];
		this.pendingImagePaths.length = 0;
		this.scrollTop = undefined;
		this.status = "Side conversation cleared";
		this.persist();
		this.requestRender();
	}

	insertPastedContent(content: string): void {
		if (IMAGE_PATH.test(content.trim())) {
			if (!this.ctx.model?.input.includes("image")) {
				this.status = "Current model does not support images";
				this.requestRender();
				return;
			}
			const path = content.trim();
			if (!this.pendingImagePaths.includes(path)) this.pendingImagePaths.push(path);
			this.status = "";
			this.requestRender();
			return;
		}
		this.input.handleInput(`\x1b[200~${content}\x1b[201~`);
		this.requestRender();
	}

	private updateProgress(status: string): void {
		if (this.disposed) return;
		this.status = status;
		this.requestRender();
	}

	private async imageContent(path: string, signal: AbortSignal): Promise<ImageContent> {
		const cached = this.imageCache.get(path);
		if (cached) return cached;
		const result = await createReadTool(this.ctx.cwd).execute(`side-image-${crypto.randomUUID()}`, { path }, signal);
		const image = result.content.find((part): part is ImageContent => part.type === "image");
		if (!image) {
			const detail = textContent(result.content).trim();
			throw new Error(detail || `Could not load image: ${path}`);
		}
		this.imageCache.set(path, image);
		return image;
	}

	private async userContent(
		text: string,
		imagePaths: string[] | undefined,
		signal: AbortSignal,
		allowMissing: boolean,
	): Promise<Array<TextContent | ImageContent>> {
		const content: Array<TextContent | ImageContent> = [
			{
				type: "text",
				text: text || (imagePaths?.length ? "Please inspect the attached image." : ""),
			},
		];
		for (const path of imagePaths ?? []) {
			try {
				content.push(await this.imageContent(path, signal));
			} catch (error) {
				if (!allowMissing) throw error;
				content.push({
					type: "text",
					text: `[Previously attached image unavailable: ${errorMessage(error)}]`,
				});
			}
		}
		return content;
	}

	private async sideMessages(
		turns: SideTurn[],
		currentUser: string,
		currentImagePaths: string[],
		signal: AbortSignal,
	): Promise<Message[]> {
		const messages: Message[] = [];
		for (const turn of turns) {
			if (!turn.assistant) continue;
			messages.push({
				role: "user",
				content: await this.userContent(turn.user, turn.imagePaths, signal, true),
				timestamp: turn.assistant.timestamp - 1,
			});
			messages.push(turn.assistant);
		}
		const current: UserMessage = {
			role: "user",
			content: await this.userContent(currentUser, currentImagePaths, signal, false),
			timestamp: Date.now(),
		};
		messages.push(current);
		return messages;
	}

	private async completeWithTools(
		priorTurns: SideTurn[],
		prompt: string,
		imagePaths: string[],
		turn: SideTurn,
		signal: AbortSignal,
	): Promise<AssistantMessage> {
		const model = this.ctx.model;
		if (!model) throw new Error("No model selected");
		const tools = createCodingTools(this.ctx.cwd);
		const toolByName = new Map(tools.map((tool) => [tool.name, tool]));
		this.updateProgress(imagePaths.length > 0 ? "Loading images…" : `Thinking with ${model.id}…`);
		const messages = await this.sideMessages(priorTurns, prompt, imagePaths, signal);
		const promptText = systemPrompt(this.ctx, this.getState().shared);
		let toolCallCount = 0;

		for (let round = 0; round < MAX_TOOL_ROUNDS; round++) {
			if (signal.aborted || this.disposed) throw new Error("Request cancelled");
			const response = await this.ctx.modelRegistry.complete(
				model,
				{ systemPrompt: promptText, messages, tools },
				{ signal },
			);
			if (response.stopReason === "error" || response.stopReason === "aborted") {
				throw new Error(response.errorMessage || `Side response ${response.stopReason}`);
			}
			messages.push(response);
			const calls = response.content.filter((part): part is ToolCall => part.type === "toolCall");
			if (calls.length === 0) return response;

			for (const call of calls) {
				if (signal.aborted || this.disposed) throw new Error("Request cancelled");
				toolCallCount++;
				if (toolCallCount > MAX_TOOL_CALLS) throw new Error(`Side tool-call limit (${MAX_TOOL_CALLS}) reached`);

				const run: SideToolRun = { name: call.name, input: toolCallDisplay(call, this.ctx.cwd) };
				(turn.tools ??= []).push(run);
				this.updateProgress(`Running ${call.name}…`);

				let resultMessage: ToolResultMessage;
				try {
					const tool = toolByName.get(call.name);
					if (!tool) throw new Error(`Unknown side tool: ${call.name}`);
					const args = validateToolCall(tools, call);
					const result = await tool.execute(call.id, args, signal);
					if (signal.aborted || this.disposed) throw new Error("Request cancelled");
					resultMessage = {
						role: "toolResult",
						toolCallId: call.id,
						toolName: call.name,
						content: result.content ?? [],
						details: result.details,
						usage: result.usage,
						isError: false,
						timestamp: Date.now(),
					};
				} catch (error) {
					if (signal.aborted || this.disposed) throw error;
					resultMessage = {
						role: "toolResult",
						toolCallId: call.id,
						toolName: call.name,
						content: [{ type: "text", text: errorMessage(error) }],
						isError: true,
						timestamp: Date.now(),
					};
				}
				messages.push(resultMessage);
				run.output = toolResultDisplay(resultMessage, call);
				run.isError = resultMessage.isError;
				this.updateProgress(resultMessage.isError ? `${call.name} failed; continuing…` : `${call.name} complete…`);
			}
		}
		throw new Error(`Side tool-round limit (${MAX_TOOL_ROUNDS}) reached`);
	}

	private async submit(raw: string): Promise<void> {
		const prompt = raw.trim();
		const imagePaths = [...this.pendingImagePaths];
		if ((!prompt && imagePaths.length === 0) || this.abortController.current) return;
		if (!this.ctx.model) {
			this.status = "No model selected";
			this.requestRender();
			return;
		}
		if (imagePaths.length > 0 && !this.ctx.model.input.includes("image")) {
			this.status = "Current model does not support images";
			this.requestRender();
			return;
		}

		this.input.setValue("");
		this.pendingImagePaths.length = 0;
		this.scrollTop = undefined;
		const state = this.getState();
		const priorTurns = [...state.turns];
		const turn: SideTurn = {
			user: prompt,
			imagePaths: imagePaths.length > 0 ? imagePaths : undefined,
		};
		state.turns.push(turn);
		if (state.turns.length > MAX_PERSISTED_TURNS) state.turns.splice(0, state.turns.length - MAX_PERSISTED_TURNS);
		this.status = `Thinking with ${this.ctx.model.id}…`;
		this.persist();
		this.requestRender();

		const controller = new AbortController();
		this.abortController.current = controller;
		try {
			const response = await this.completeWithTools(priorTurns, prompt, imagePaths, turn, controller.signal);
			turn.assistant = response;
			this.status = response.stopReason === "length" ? "Reply reached its length limit" : "";
		} catch (error) {
			turn.error = controller.signal.aborted || this.disposed ? "Request cancelled" : errorMessage(error);
			this.status = turn.error;
		} finally {
			if (this.abortController.current === controller) this.abortController.current = undefined;
			if (!this.disposed) {
				if (this.clearOnSettle) {
					this.clearOnSettle = false;
					this.getState().turns = [];
					this.scrollTop = undefined;
					this.status = "Side conversation cleared";
				}
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

	private scrollBy(lines: number): void {
		const maxScrollTop = Math.max(0, this.transcriptLineCount - this.transcriptHeight);
		const current = this.scrollTop ?? maxScrollTop;
		const next = Math.max(0, Math.min(maxScrollTop, current + lines));
		this.scrollTop = next >= maxScrollTop ? undefined : next;
		this.requestRender();
	}

	private installWheelRoute(): void {
		const tui = this.tui as WheelRoutableTui;
		const previous = tui.routeWheel;
		if (typeof previous !== "function") return;

		const routeWheel = (event: WheelEvent) => {
			const panelLeft = Math.max(0, this.tui.terminal.columns - this.panelWidth);
			const insidePanel =
				this.panelWidth > 0 && event.x >= panelLeft && event.y >= 0 && event.y < this.tui.terminal.rows;
			if (insidePanel) {
				this.scrollBy(event.direction * 3);
				return;
			}
			previous.call(this.tui, event);
		};
		tui.routeWheel = routeWheel;
		this.restoreWheelRoute = () => {
			if (tui.routeWheel === routeWheel) tui.routeWheel = previous;
		};
	}

	handleInput(data: string): void {
		if (this.keybindings.matches(data, "app.clipboard.pasteImage")) {
			this.requestClipboardPaste();
			return;
		}
		if (matchesKey(data, SHORTCUT)) {
			this.toggleFocus();
			return;
		}
		if (matchesKey(data, "backspace") && this.input.getValue() === "" && this.pendingImagePaths.length > 0) {
			this.pendingImagePaths.pop();
			this.status = "";
			this.requestRender();
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
		if (matchesKey(data, "alt+l")) {
			this.clearConversation();
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

	private wrappedTool(tool: SideToolRun, width: number): string[] {
		const name = this.theme.fg("toolTitle", this.theme.bold(tool.name));
		const input = this.theme.fg("muted", tool.input);
		let status = this.theme.fg("warning", "…");
		if (tool.output) {
			const marker = tool.isError ? "✗" : "✓";
			const color = tool.isError ? "error" : "success";
			status = this.theme.fg(color, `${marker} ${tool.output}`);
		}
		return wrapTextWithAnsi(`${name}  ${input}  ${status}`, width);
	}

	render(width: number): string[] {
		this.panelWidth = width;
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
			const imageLabel = turn.imagePaths?.length
				? `[${turn.imagePaths.length} attached ${turn.imagePaths.length === 1 ? "image" : "images"}]`
				: "";
			transcript.push(
				...this.wrappedMessage("You", [turn.user, imageLabel].filter(Boolean).join("\n"), innerWidth, "accent"),
			);
			for (const tool of turn.tools ?? []) transcript.push(...this.wrappedTool(tool, innerWidth));
			const reply = assistantText(turn.assistant);
			if (turn.assistant)
				transcript.push(...this.wrappedMessage("Side", reply || "(No text reply)", innerWidth, "text"));
			else if (turn.error) transcript.push(...this.wrappedMessage("Error", turn.error, innerWidth, "error"));
			else transcript.push(this.theme.fg("warning", this.theme.bold("Side")), this.theme.fg("warning", "Thinking…"));
			transcript.push("");
		}

		// Seven rows are used by the header, separator, input, status/help, and footer.
		// Fill every remaining terminal row so the overlay behaves like a full-height sidebar.
		const transcriptHeight = Math.max(1, this.tui.terminal.rows - 7);
		this.transcriptLineCount = transcript.length;
		this.transcriptHeight = transcriptHeight;
		const maxScrollTop = Math.max(0, transcript.length - transcriptHeight);
		const scrollTop = Math.max(0, Math.min(maxScrollTop, this.scrollTop ?? maxScrollTop));
		if (this.scrollTop !== undefined) this.scrollTop = scrollTop >= maxScrollTop ? undefined : scrollTop;
		const visibleTranscript = transcript.slice(scrollTop, scrollTop + transcriptHeight);
		for (let index = visibleTranscript.length; index < transcriptHeight; index++) {
			lines.push(border("│") + " ".repeat(innerWidth) + border("│"));
		}
		for (const line of visibleTranscript) lines.push(border("│") + this.padded(line, innerWidth) + border("│"));

		lines.push(border("├") + border("─".repeat(innerWidth)) + border("┤"));
		const [renderedInput = ""] = this.input.render(innerWidth);
		const inputLine = this.focused && this.hasTerminalFocus() ? renderedInput : hideFakeCursor(renderedInput);
		lines.push(border("│") + this.padded(inputLine, innerWidth) + border("│"));
		const statusParts: string[] = [];
		if (maxScrollTop > 0)
			statusParts.push(
				`${scrollTop + 1}–${Math.min(transcript.length, scrollTop + transcriptHeight)}/${transcript.length}`,
			);
		if (this.pendingImagePaths.length > 0) {
			statusParts.push(
				`${this.pendingImagePaths.length} ${this.pendingImagePaths.length === 1 ? "image" : "images"} ready · Backspace removes`,
			);
		}
		if (this.status) statusParts.push(this.status);
		const status = statusParts.length > 0 ? ` ${statusParts.join(" · ")}` : "";
		lines.push(border("│") + this.padded(this.theme.fg("dim", status), innerWidth) + border("│"));
		lines.push(
			border("│") + this.padded(this.theme.fg("dim", " Enter send · Ctrl+V image · ← main"), innerWidth) + border("│"),
		);
		lines.push(
			border("│") +
				this.padded(this.theme.fg("dim", " Alt+L clear · Alt+C context · Esc close"), innerWidth) +
				border("│"),
		);
		lines.push(border(`╰${"─".repeat(innerWidth)}╯`));
		return lines;
	}

	invalidate(): void {
		this.input.invalidate();
	}

	dispose(): void {
		if (this.disposed) return;
		this.disposed = true;
		this.restoreWheelRoute?.();
		this.restoreWheelRoute = undefined;
		this.abortController.current?.abort();
		this.abortController.current = undefined;
	}
}

export default function sideConversationExtension(pi: ExtensionAPI) {
	let state = emptyState();
	let active: ActivePanel | undefined;
	let restoreEditor: (() => void) | undefined;
	let boundaryEditor: SidebarBoundaryEditor | undefined;
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
		panel.panel.dispose();
		panel.close();
	};

	const installBoundaryEditor = (ctx: ExtensionContext) => {
		if (editorInstalled || ctx.mode !== "tui") return;
		const previousEditor = ctx.ui.getEditorComponent();
		ctx.ui.setEditorComponent((tui, theme, keybindings) => {
			const base = previousEditor?.(tui, theme, keybindings) ?? new CustomEditor(tui, theme, keybindings);
			boundaryEditor = new SidebarBoundaryEditor(
				base,
				() => {
					if (!active) return false;
					active.panel.focus();
					return true;
				},
				(content) => {
					if (!active?.panel.focused) return false;
					active.panel.insertPastedContent(content);
					return true;
				},
			);
			return boundaryEditor;
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
			(tui, theme, keybindings, done) => {
				doneOverlay = () => done();
				panel = new SideConversationPanel(
					tui,
					theme,
					keybindings,
					ctx,
					() => state,
					() => terminalFocused,
					() => boundaryEditor?.onPasteImage?.(),
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
				panel?.dispose();
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
		boundaryEditor = undefined;
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
				if (active) active.panel.clearConversation();
				else {
					state.turns = [];
					persist();
				}
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
