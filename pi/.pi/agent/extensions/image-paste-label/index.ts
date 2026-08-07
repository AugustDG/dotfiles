import { basename } from "node:path";
import { CustomEditor, type ExtensionAPI, type KeybindingsManager } from "@earendil-works/pi-coding-agent";
import type { AutocompleteProvider, Component, EditorComponent, EditorTheme, TUI } from "@earendil-works/pi-tui";

const CLIPBOARD_FILE_NAME = /^pi-clipboard-[0-9a-f-]+\.(?:png|jpe?g|gif|webp|bmp)$/i;
const CLIPBOARD_PATH = /(?:\/[^\s`"'<>]+)*\/pi-clipboard-[0-9a-f-]+\.(?:png|jpe?g|gif|webp|bmp)/gi;
const IMAGE_MARKER = /\[Image #(\d+)\]/g;

interface AppAwareEditor extends EditorComponent {
	actionHandlers?: Map<unknown, () => void>;
	onEscape?: () => void;
	onCtrlD?: () => void;
	onPasteImage?: () => void;
	onExtensionShortcut?: (data: string) => boolean;
	focused?: boolean;
	wantsKeyRelease?: boolean;
	disableSubmit?: boolean;
	dispose?: () => void;
}

class ImageLabelEditor implements EditorComponent {
	private readonly images = new Map<number, string>();
	private nextImageId = 1;

	constructor(
		private readonly base: AppAwareEditor,
		private readonly accent: (text: string) => string,
	) {}

	get actionHandlers(): Map<unknown, () => void> | undefined {
		return this.base.actionHandlers;
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

	get onEscape(): (() => void) | undefined {
		return this.base.onEscape;
	}
	set onEscape(value: (() => void) | undefined) {
		this.base.onEscape = value;
	}

	get onCtrlD(): (() => void) | undefined {
		return this.base.onCtrlD;
	}
	set onCtrlD(value: (() => void) | undefined) {
		this.base.onCtrlD = value;
	}

	get onPasteImage(): (() => void) | undefined {
		return this.base.onPasteImage;
	}
	set onPasteImage(value: (() => void) | undefined) {
		this.base.onPasteImage = value;
	}

	get onExtensionShortcut(): ((data: string) => boolean) | undefined {
		return this.base.onExtensionShortcut;
	}
	set onExtensionShortcut(value: ((data: string) => boolean) | undefined) {
		this.base.onExtensionShortcut = value;
	}

	get focused(): boolean {
		return this.base.focused ?? false;
	}
	set focused(value: boolean) {
		this.base.focused = value;
	}

	get wantsKeyRelease(): boolean | undefined {
		return this.base.wantsKeyRelease;
	}

	get disableSubmit(): boolean | undefined {
		return this.base.disableSubmit;
	}
	set disableSubmit(value: boolean | undefined) {
		this.base.disableSubmit = value;
	}

	get borderColor(): ((text: string) => string) | undefined {
		return this.base.borderColor;
	}
	set borderColor(value: ((text: string) => string) | undefined) {
		this.base.borderColor = value;
	}

	private registerImage(path: string): string {
		const existing = [...this.images.entries()].find(([, imagePath]) => imagePath === path);
		if (existing) return `[Image #${existing[0]}]`;
		const id = this.nextImageId++;
		this.images.set(id, path);
		return `[Image #${id}]`;
	}

	private collapseClipboardPaths(text: string): string {
		return text.replace(CLIPBOARD_PATH, (path) => this.registerImage(path));
	}

	expandImageMarkers(text: string, reset: boolean): string {
		const expanded = text.replace(IMAGE_MARKER, (marker, rawId: string) => {
			const path = this.images.get(Number.parseInt(rawId, 10));
			return path ?? marker;
		});
		if (reset) {
			this.images.clear();
			this.nextImageId = 1;
		}
		return expanded;
	}

	render(width: number): string[] {
		return this.base.render(width).map((line) => line.replace(IMAGE_MARKER, (marker) => this.accent(marker)));
	}

	invalidate(): void {
		this.base.invalidate();
	}

	handleInput(data: string): void {
		this.base.handleInput(data);
	}

	getText(): string {
		return this.base.getText();
	}

	getExpandedText(): string {
		const text = this.base.getExpandedText?.() ?? this.base.getText();
		return this.expandImageMarkers(text, false);
	}

	setText(text: string): void {
		this.base.setText(this.collapseClipboardPaths(text));
	}

	addToHistory(text: string): void {
		this.base.addToHistory?.(text);
	}

	insertTextAtCursor(text: string): void {
		const inserted = CLIPBOARD_FILE_NAME.test(basename(text)) ? this.registerImage(text) : text;
		this.base.insertTextAtCursor?.(inserted);
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
		this.base.dispose?.();
	}
}

export default function imagePasteLabel(pi: ExtensionAPI) {
	let activeEditor: ImageLabelEditor | undefined;

	pi.on("session_start", (_event, ctx) => {
		if (ctx.mode !== "tui") return;
		const previousFactory = ctx.ui.getEditorComponent();
		ctx.ui.setEditorComponent((tui: TUI, editorTheme: EditorTheme, keybindings: KeybindingsManager) => {
			const base = previousFactory
				? (previousFactory(tui, editorTheme, keybindings) as AppAwareEditor)
				: new CustomEditor(tui, editorTheme, keybindings);
			activeEditor = new ImageLabelEditor(base, (text) => ctx.ui.theme.fg("accent", text));
			return activeEditor;
		});
	});

	pi.on("input", (event) => {
		if (event.source !== "interactive" || !activeEditor) return;
		const text = activeEditor.expandImageMarkers(event.text, true);
		if (text !== event.text) return { action: "transform", text, images: event.images };
		return { action: "continue" };
	});

	pi.registerMarkdownTransformer((markdown, { messageType }) => {
		if (messageType !== "user") return markdown;
		let imageNumber = 0;
		return markdown.replace(CLIPBOARD_PATH, () => `\`[Image #${++imageNumber}]\``);
	});

	pi.on("session_shutdown", () => {
		activeEditor = undefined;
	});
}
