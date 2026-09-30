import {
	CustomEditor,
	type ExtensionAPI,
	type ExtensionContext,
	type KeybindingsManager,
	type ReadonlyFooterDataProvider,
} from "@earendil-works/pi-coding-agent";
import type { Component, EditorTheme, TUI } from "@earendil-works/pi-tui";
import { matchesKey, truncateToWidth, visibleWidth } from "@earendil-works/pi-tui";
import { basename } from "node:path";
import { getPoName } from "../session-messages/index";

const SPINNER_FRAMES = ["·", "✢", "✳", "✢"];
const BASE_THEME = "claude-forest";
type ColorMode = "truecolor" | "256color";

interface Rgb {
	r: number;
	g: number;
	b: number;
}

interface SessionPalette {
	accent: string;
	backgrounds: Record<string, string>;
}

interface MutableThemeInternals {
	fgColors?: Map<string, string>;
	bgColors?: Map<string, string>;
}

function hashString(value: string): number {
	let hash = 0x811c9dc5;
	for (let index = 0; index < value.length; index++) {
		hash ^= value.charCodeAt(index);
		hash = Math.imul(hash, 0x01000193);
	}
	return hash >>> 0;
}

function hslToRgb(hue: number, saturation: number, lightness: number): Rgb {
	const s = saturation / 100;
	const l = lightness / 100;
	const chroma = (1 - Math.abs(2 * l - 1)) * s;
	const section = (((hue % 360) + 360) % 360) / 60;
	const x = chroma * (1 - Math.abs((section % 2) - 1));
	const [red, green, blue] =
		section < 1
			? [chroma, x, 0]
			: section < 2
				? [x, chroma, 0]
				: section < 3
					? [0, chroma, x]
					: section < 4
						? [0, x, chroma]
						: section < 5
							? [x, 0, chroma]
							: [chroma, 0, x];
	const match = l - chroma / 2;
	return {
		r: Math.round((red + match) * 255),
		g: Math.round((green + match) * 255),
		b: Math.round((blue + match) * 255),
	};
}

function sessionColor(identity: string): Rgb {
	return hslToRgb(hashString(identity.trim().toLowerCase()) % 360, 52, 35);
}

function ansi256Index(color: Rgb): number {
	const red = Math.round((color.r / 255) * 5);
	const green = Math.round((color.g / 255) * 5);
	const blue = Math.round((color.b / 255) * 5);
	return 16 + 36 * red + 6 * green + blue;
}

function foregroundAnsi(color: Rgb, mode: ColorMode): string {
	return mode === "256color" ? `\x1b[38;5;${ansi256Index(color)}m` : `\x1b[38;2;${color.r};${color.g};${color.b}m`;
}

function backgroundAnsi(color: Rgb, mode: ColorMode): string {
	return mode === "256color" ? `\x1b[48;5;${ansi256Index(color)}m` : `\x1b[48;2;${color.r};${color.g};${color.b}m`;
}

function blendWithWhite(color: Rgb, whiteRatio: number): Rgb {
	const blend = (channel: number) => Math.round(channel * (1 - whiteRatio) + 255 * whiteRatio);
	return { r: blend(color.r), g: blend(color.g), b: blend(color.b) };
}

function createSessionPalette(identity: string, mode: ColorMode): SessionPalette {
	const color = sessionColor(identity);
	return {
		accent: foregroundAnsi(color, mode),
		backgrounds: {
			selectedBg: backgroundAnsi(blendWithWhite(color, 0.84), mode),
			scrollbarThumb: backgroundAnsi(blendWithWhite(color, 0.68), mode),
			searchMatchBg: backgroundAnsi(blendWithWhite(color, 0.8), mode),
			userMessageBg: backgroundAnsi(blendWithWhite(color, 0.94), mode),
			customMessageBg: backgroundAnsi(blendWithWhite(color, 0.91), mode),
			toolPendingBg: backgroundAnsi(blendWithWhite(color, 0.94), mode),
			toolSuccessBg: backgroundAnsi(blendWithWhite(color, 0.88), mode),
		},
	};
}

function patchSessionTheme(theme: ExtensionContext["ui"]["theme"], palette: SessionPalette): boolean {
	const mutable = theme as unknown as MutableThemeInternals;
	if (!mutable.fgColors || !mutable.bgColors) return false;
	let changed = false;
	for (const token of [
		"accent",
		"border",
		"borderAccent",
		"customMessageLabel",
		"toolTitle",
		"toolOutput",
		"thinkingText",
		"mdLink",
		"mdCode",
		"mdListBullet",
	]) {
		if (mutable.fgColors.get(token) === palette.accent) continue;
		mutable.fgColors.set(token, palette.accent);
		changed = true;
	}
	for (const [token, ansi] of Object.entries(palette.backgrounds)) {
		if (mutable.bgColors.get(token) === ansi) continue;
		mutable.bgColors.set(token, ansi);
		changed = true;
	}
	return changed;
}

function formatPath(cwd: string): string {
	const home = process.env.HOME;
	return home && cwd.startsWith(home) ? `~${cwd.slice(home.length)}` : cwd;
}

function formatContext(ctx: ExtensionContext): string {
	const usage = ctx.getContextUsage();
	if (!usage || usage.percent === null) return "ctx —";
	return `${Math.max(0, 100 - Math.round(usage.percent))}% ctx`;
}

function formatElapsed(startedAt: number | undefined): string {
	if (!startedAt) return "0s";
	const totalSeconds = Math.max(0, Math.floor((Date.now() - startedAt) / 1000));
	if (totalSeconds < 60) return `${totalSeconds}s`;
	const minutes = Math.floor(totalSeconds / 60);
	const seconds = totalSeconds % 60;
	return `${minutes}m${seconds.toString().padStart(2, "0")}s`;
}

function fitBorder(
	left: string,
	right: string,
	width: number,
	border: (text: string) => string,
	fill: (text: string) => string = border,
): string {
	if (width <= 0) return "";
	if (width === 1) return border("─");

	let leftText = left;
	let rightText = right;
	const frameWidth = 2;
	const minimumGap = 3;
	while (
		frameWidth + visibleWidth(leftText) + visibleWidth(rightText) + minimumGap > width &&
		visibleWidth(rightText) > 0
	) {
		rightText = truncateToWidth(rightText, Math.max(0, visibleWidth(rightText) - 1), "");
	}
	while (
		frameWidth + visibleWidth(leftText) + visibleWidth(rightText) + minimumGap > width &&
		visibleWidth(leftText) > 0
	) {
		leftText = truncateToWidth(leftText, Math.max(0, visibleWidth(leftText) - 1), "");
	}
	const gap = Math.max(0, width - frameWidth - visibleWidth(leftText) - visibleWidth(rightText));
	return `${border("─")}${leftText}${fill("─".repeat(gap))}${rightText}${border("─")}`;
}

function fitBottomBorder(left: string, right: string, width: number, border: (text: string) => string): string {
	return fitBorder(left, right, width, border);
}

class EmptyFooter implements Component {
	render(): string[] {
		return [];
	}

	invalidate(): void {}
}

export default function poGreenUi(pi: ExtensionAPI) {
	let activeTui: TUI | undefined;
	let activeContext: ExtensionContext | undefined;
	let footerData: ReadonlyFooterDataProvider | undefined;
	let branch: string | undefined;
	let isWorking = false;
	let workingStartedAt: number | undefined;
	let spinnerIndex = 0;
	let spinnerTimer: ReturnType<typeof setInterval> | undefined;
	let sessionPalette: SessionPalette | undefined;
	let sessionAccent = (text: string) => text;
	const activeTools = new Map<string, { name: string; startedAt: number }>();

	const updateSessionAccent = (ctx: ExtensionContext) => {
		const identity = getPoName(ctx);
		sessionPalette = createSessionPalette(identity, ctx.ui.theme.getColorMode());
		sessionAccent = (text) => `${sessionPalette?.accent ?? ""}${text}\x1b[39m`;
		patchSessionTheme(ctx.ui.theme, sessionPalette);
		activeTui?.requestRender();
	};

	const stopSpinner = () => {
		if (!spinnerTimer) return;
		clearInterval(spinnerTimer);
		spinnerTimer = undefined;
	};

	const refreshBranch = async () => {
		const ctx = activeContext;
		if (!ctx) return;
		const result = await pi.exec("git", ["branch", "--show-current"], { cwd: ctx.cwd }).catch(() => undefined);
		const next = result?.stdout.trim();
		branch = next || undefined;
		activeTui?.requestRender();
	};

	const startWorking = () => {
		if (!isWorking) {
			workingStartedAt = Date.now();
			activeTools.clear();
		}
		isWorking = true;
		stopSpinner();
		spinnerTimer = setInterval(() => {
			spinnerIndex = (spinnerIndex + 1) % SPINNER_FRAMES.length;
			activeTui?.requestRender();
		}, 140);
		activeTui?.requestRender();
	};

	const stopWorking = () => {
		isWorking = false;
		workingStartedAt = undefined;
		activeTools.clear();
		stopSpinner();
		activeTui?.requestRender();
	};

	pi.on("agent_start", startWorking);
	pi.on("agent_settled", async () => {
		stopWorking();
		await refreshBranch();
	});
	pi.on("tool_execution_start", (event) => {
		activeTools.set(event.toolCallId, { name: event.toolName, startedAt: Date.now() });
		activeTui?.requestRender();
	});
	pi.on("tool_execution_end", (event) => {
		activeTools.delete(event.toolCallId);
		activeTui?.requestRender();
	});
	pi.on("model_select", () => activeTui?.requestRender());
	pi.on("thinking_level_select", () => activeTui?.requestRender());
	pi.on("session_info_changed", () => activeTui?.requestRender());

	pi.on("session_start", async (_event, ctx) => {
		if (ctx.mode !== "tui") return;
		activeContext = ctx;
		branch = undefined;
		const themeResult = ctx.ui.setTheme(BASE_THEME);
		if (!themeResult.success) {
			ctx.ui.notify(`Could not load ${BASE_THEME} theme: ${themeResult.error}`, "error");
		}
		updateSessionAccent(ctx);
		ctx.ui.setWorkingVisible(false);
		ctx.ui.setWorkingIndicator({ frames: [] });
		ctx.ui.setHiddenThinkingLabel("Thinking…");
		ctx.ui.setToolsExpanded(false);
		ctx.ui.setFooter((_tui, _theme, data) => {
			footerData = data;
			return new EmptyFooter();
		});
		ctx.ui.setTitle(`po · ${basename(ctx.cwd) || "workspace"}`);
		await refreshBranch();

		class ClaudeForestEditor extends CustomEditor {
			constructor(tui: TUI, theme: EditorTheme, keybindings: KeybindingsManager) {
				super(tui, theme, keybindings, { paddingX: 2 });
				activeTui = tui;
			}

			handleInput(data: string): void {
				if (matchesKey(data, "up") && this.getText().length === 0 && !this.isShowingAutocomplete() && ctx.hasPendingMessages()) {
					const dequeue = this.actionHandlers.get("app.message.dequeue");
					if (dequeue) {
						dequeue();
						return;
					}
				}
				super.handleInput(data);
			}

			render(width: number): string[] {
				this.borderColor = sessionAccent;
				const lines = super.render(width);
				if (lines.length < 2 || activeContext !== ctx) return lines;

				const theme = ctx.ui.theme;
				if (sessionPalette && patchSessionTheme(theme, sessionPalette)) {
					queueMicrotask(() => activeTui?.requestRender());
				}
				const model = ctx.model?.id ?? "no model";
				const thinking = pi.getThinkingLevel();
				const runningTools = [...activeTools.values()];
				const currentTool = runningTools.at(-1);
				const activity = currentTool
					? `${currentTool.name}${runningTools.length > 1 ? ` +${runningTools.length - 1}` : ""}`
					: "thinking";
				const sessionName = getPoName(ctx);
				const working = sessionAccent(
					theme.bold(
						isWorking
							? ` ${sessionName} · ${SPINNER_FRAMES[spinnerIndex]} ${activity} ${formatElapsed(workingStartedAt)} `
							: ` ${sessionName} `,
					),
				);
				const separator = theme.fg("dim", " · ");
				const backgroundStatus = footerData?.getExtensionStatuses().get("background-tasks");
				const taskStatus = backgroundStatus ? `${theme.fg("warning", backgroundStatus)}${separator}` : "";
				const modelStatus = ` ${taskStatus}${sessionAccent(theme.bold(model))}${separator}${theme.fg("warning", thinking)} `;
				const location = ` ${sessionAccent(formatContext(ctx))}${separator}${theme.fg("muted", formatPath(ctx.cwd))}${branch ? `${separator}${sessionAccent(branch)}` : ""} `;
				lines[0] = fitBorder(working, "", width, sessionAccent);
				lines[lines.length - 1] = fitBottomBorder(modelStatus, location, width, sessionAccent);
				return lines;
			}
		}

		ctx.ui.setEditorComponent((tui, theme, keybindings) => new ClaudeForestEditor(tui, theme, keybindings));
	});

	pi.on("session_shutdown", () => {
		activeContext = undefined;
		footerData = undefined;
		stopSpinner();
		activeTui = undefined;
	});
}
