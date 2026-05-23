package tui

import (
	"fmt"
	"strings"

	"github.com/AugustDG/dotfiles/internal/config"
	"github.com/AugustDG/dotfiles/internal/platform"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	disabledStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	cursorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	hintStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	descStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
)

type PickerModel struct {
	modules  []config.Module
	cursor   int
	selected map[int]bool
	os       string
	done     bool
}

type PickerDoneMsg struct {
	Selected []config.Module
}

func NewPickerModel(modules []config.Module) PickerModel {
	selected := make(map[int]bool)
	currentOS := platform.DetectOS()
	for i, m := range modules {
		if m.IsStowed && m.SupportsOS(currentOS) {
			selected[i] = true
		}
	}
	return PickerModel{
		modules:  modules,
		selected: selected,
		os:       currentOS,
	}
}

func (m PickerModel) Init() tea.Cmd {
	return nil
}

func (m PickerModel) Update(msg tea.Msg) (PickerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			m.cursor = (m.cursor + 1) % len(m.modules)
		case "k", "up":
			m.cursor = (m.cursor - 1 + len(m.modules)) % len(m.modules)
		case " ":
			if m.modules[m.cursor].SupportsOS(m.os) {
				m.selected[m.cursor] = !m.selected[m.cursor]
			}
		case "a":
			for i, mod := range m.modules {
				if mod.SupportsOS(m.os) {
					m.selected[i] = true
				}
			}
		case "n":
			m.selected = make(map[int]bool)
		case "enter":
			m.done = true
			var sel []config.Module
			for i, mod := range m.modules {
				if m.selected[i] {
					sel = append(sel, mod)
				}
			}
			return m, func() tea.Msg { return PickerDoneMsg{Selected: sel} }
		}
	}
	return m, nil
}

func (m PickerModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Select modules to install"))
	b.WriteString("\n\n")

	for i, mod := range m.modules {
		cursor := "  "
		if m.cursor == i {
			cursor = cursorStyle.Render("> ")
		}

		supported := mod.SupportsOS(m.os)
		checked := "[ ]"
		if m.selected[i] {
			checked = selectedStyle.Render("[x]")
		}

		name := mod.Name
		desc := descStyle.Render(mod.Description)

		if !supported {
			checked = disabledStyle.Render("[-]")
			name = disabledStyle.Render(mod.Name)
			desc = disabledStyle.Render(fmt.Sprintf("%s (requires %s)", mod.Description, strings.Join(mod.OS, "/")))
		}

		deps := ""
		if len(mod.Deps.Brew) > 0 {
			deps = hintStyle.Render(fmt.Sprintf(" [brew: %s]", strings.Join(mod.Deps.Brew, ", ")))
		}

		b.WriteString(fmt.Sprintf("%s%s %s%s  %s\n", cursor, checked, name, deps, desc))
	}

	b.WriteString("\n")
	b.WriteString(hintStyle.Render("j/k: navigate  space: toggle  a: all  n: none  enter: confirm  q: quit"))
	return b.String()
}
