import {
	CustomEditor,
	type ExtensionAPI,
	type ExtensionContext,
	type KeybindingsManager,
} from "@earendil-works/pi-coding-agent";
import type { Component, EditorTheme, TUI } from "@earendil-works/pi-tui";
import { truncateToWidth, visibleWidth } from "@earendil-works/pi-tui";
import { basename } from "node:path";

const SPINNER_FRAMES = ["·", "✢", "✳", "✢"];

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

function fitBottomBorder(
	left: string,
	right: string,
	width: number,
	border: (text: string) => string,
): string {
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
	let branch: string | undefined;
	let isWorking = false;
	let workingStartedAt: number | undefined;
	let spinnerIndex = 0;
	let spinnerTimer: ReturnType<typeof setInterval> | undefined;
	const activeTools = new Map<string, { name: string; startedAt: number }>();

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

	pi.on("session_start", async (_event, ctx) => {
		if (ctx.mode !== "tui") return;
		activeContext = ctx;
		branch = undefined;
		const themeResult = ctx.ui.setTheme("claude-forest");
		if (!themeResult.success) {
			ctx.ui.notify(`Could not load claude-forest theme: ${themeResult.error}`, "error");
		}
		ctx.ui.setWorkingVisible(false);
		ctx.ui.setWorkingIndicator({ frames: [] });
		ctx.ui.setHiddenThinkingLabel("Thinking…");
		ctx.ui.setToolsExpanded(false);
		ctx.ui.setFooter(() => new EmptyFooter());
		ctx.ui.setTitle(`po · ${basename(ctx.cwd) || "workspace"}`);
		await refreshBranch();

		class ClaudeForestEditor extends CustomEditor {
			constructor(tui: TUI, theme: EditorTheme, keybindings: KeybindingsManager) {
				super(tui, theme, keybindings, { paddingX: 2 });
				activeTui = tui;
			}

			render(width: number): string[] {
				const lines = super.render(width);
				if (lines.length < 2 || activeContext !== ctx) return lines;

				const theme = ctx.ui.theme;
				const model = ctx.model?.id ?? "no model";
				const thinking = pi.getThinkingLevel();
				const runningTools = [...activeTools.values()];
				const currentTool = runningTools.at(-1);
				const activity = currentTool
					? `${currentTool.name}${runningTools.length > 1 ? ` +${runningTools.length - 1}` : ""}`
					: "thinking";
				const working = isWorking
					? theme.fg(
							"accent",
							theme.bold(` ${SPINNER_FRAMES[spinnerIndex]} ${activity} ${formatElapsed(workingStartedAt)} `),
						)
					: theme.fg("accent", theme.bold(" po "));
				const separator = theme.fg("dim", " · ");
				const modelStatus = ` ${theme.fg("accent", theme.bold(model))}${separator}${theme.fg("warning", thinking)} `;
				const location = ` ${theme.fg("success", formatContext(ctx))}${separator}${theme.fg("muted", formatPath(ctx.cwd))}${branch ? `${separator}${theme.fg("accent", branch)}` : ""} `;
				const border = (text: string) => this.borderColor(text);
				lines[0] = fitBorder(working, "", width, border);
				lines[lines.length - 1] = fitBottomBorder(modelStatus, location, width, border);
				return lines;
			}
		}

		ctx.ui.setEditorComponent(
			(tui, theme, keybindings) => new ClaudeForestEditor(tui, theme, keybindings),
		);
	});

	pi.on("session_shutdown", () => {
		activeContext = undefined;
		stopSpinner();
		activeTui = undefined;
	});
}
