package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Palette. Every style in the CLI derives from these ANSI colors so output
// stays consistent and follows the user's terminal theme.
var (
	colorAccent = lipgloss.Color("12")
	colorActive = lipgloss.Color("14")
	colorOK     = lipgloss.Color("10")
	colorWarn   = lipgloss.Color("11")
	colorFail   = lipgloss.Color("9")
	colorText   = lipgloss.Color("7")
	colorMuted  = lipgloss.Color("8")
)

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	groupStyle  = lipgloss.NewStyle().Bold(true)
	accentStyle = lipgloss.NewStyle().Foreground(colorAccent)
	activeStyle = lipgloss.NewStyle().Foreground(colorActive)
	okStyle     = lipgloss.NewStyle().Foreground(colorOK)
	warnStyle   = lipgloss.NewStyle().Foreground(colorWarn)
	failStyle   = lipgloss.NewStyle().Foreground(colorFail)
	textStyle   = lipgloss.NewStyle().Foreground(colorText)
	mutedStyle  = lipgloss.NewStyle().Foreground(colorMuted)
)

// Icons.
const (
	IconOK      = "✓"
	IconWarn    = "⚠"
	IconFail    = "✗"
	IconSkip    = "–"
	IconInfo    = "·"
	IconPending = "·"
	IconActive  = "●"
	IconHint    = "→"
	IconCursor  = "›"
)

// Level is the outcome of a check, task, or message.
type Level int

const (
	LevelOK Level = iota
	LevelWarn
	LevelFail
	LevelSkip
	LevelInfo
)

// Icon returns the level's styled icon.
func (l Level) Icon() string {
	switch l {
	case LevelWarn:
		return warnStyle.Render(IconWarn)
	case LevelFail:
		return failStyle.Render(IconFail)
	case LevelSkip:
		return mutedStyle.Render(IconSkip)
	case LevelInfo:
		return mutedStyle.Render(IconInfo)
	default:
		return okStyle.Render(IconOK)
	}
}

// style returns the color a level's detail text is rendered in.
func (l Level) style() lipgloss.Style {
	switch l {
	case LevelWarn:
		return warnStyle
	case LevelFail:
		return failStyle
	case LevelOK:
		return okStyle
	default:
		return mutedStyle
	}
}

// Title renders a screen heading.
func Title(s string) string { return titleStyle.Render(s) }

// Muted renders secondary text.
func Muted(s string) string { return mutedStyle.Render(s) }

// Rule renders a horizontal separator of the given width.
func Rule(width int) string { return mutedStyle.Render(strings.Repeat("─", width)) }

// KeyHints renders alternating key/action pairs, e.g. KeyHints("q", "quit").
func KeyHints(pairs ...string) string {
	parts := make([]string, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, textStyle.Render(pairs[i])+" "+mutedStyle.Render(pairs[i+1]))
	}
	return strings.Join(parts, mutedStyle.Render("  ·  "))
}

// pad left-aligns a styled string to a visible width; %-Ns would count ANSI
// escape bytes.
func pad(s string, width int) string {
	if visible := lipgloss.Width(s); visible < width {
		return s + strings.Repeat(" ", width-visible)
	}
	return s
}
