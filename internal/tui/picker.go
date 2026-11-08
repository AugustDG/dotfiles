package tui

import (
	"fmt"
	"strings"

	"github.com/AugustDG/dotfiles/internal/config"
	"github.com/AugustDG/dotfiles/internal/platform"
	tea "github.com/charmbracelet/bubbletea"
)

// PickModules shows a multi-select list of modules, preselecting the ones
// already linked. It returns the selection and whether the user confirmed it
// (enter) rather than quitting (q/esc/ctrl+c).
func PickModules(modules []config.Module) ([]config.Module, bool, error) {
	if len(modules) == 0 {
		return nil, true, nil
	}
	final, err := tea.NewProgram(newPickerModel(modules)).Run()
	if err != nil {
		return nil, false, err
	}
	m := final.(pickerModel)
	if !m.confirmed {
		return nil, false, nil
	}
	var sel []config.Module
	for i, mod := range m.modules {
		if m.selected[i] {
			sel = append(sel, mod)
		}
	}
	return sel, true, nil
}

type pickerModel struct {
	modules   []config.Module
	cursor    int
	selected  map[int]bool
	os        string
	confirmed bool
	quitting  bool
}

func newPickerModel(modules []config.Module) pickerModel {
	currentOS := platform.DetectOS()
	selected := make(map[int]bool)
	for i, m := range modules {
		if m.IsStowed && m.SupportsOS(currentOS) {
			selected[i] = true
		}
	}
	return pickerModel{modules: modules, selected: selected, os: currentOS}
}

func (m pickerModel) Init() tea.Cmd { return nil }

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "q", "esc", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "enter":
		m.confirmed = true
		return m, tea.Quit
	case "j", "down":
		m.cursor = (m.cursor + 1) % len(m.modules)
	case "k", "up":
		m.cursor = (m.cursor - 1 + len(m.modules)) % len(m.modules)
	case " ", "x":
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
	}
	return m, nil
}

func (m pickerModel) View() string {
	// Clear the list once a choice is made; the next screen takes over.
	if m.confirmed || m.quitting {
		return ""
	}

	width := 0
	for _, mod := range m.modules {
		width = max(width, len(mod.Name))
	}

	var b strings.Builder
	b.WriteString(Title("Select modules to link") + "\n\n")
	for i, mod := range m.modules {
		cursor := "  "
		if m.cursor == i {
			cursor = activeStyle.Render(IconCursor) + " "
		}

		box, name := "[ ]", pad(mod.Name, width)
		desc := textStyle.Render(mod.Description)
		switch {
		case !mod.SupportsOS(m.os):
			box = mutedStyle.Render("[-]")
			name = mutedStyle.Render(name)
			desc = mutedStyle.Render(fmt.Sprintf("%s (requires %s)", mod.Description, strings.Join(mod.OS, "/")))
		case m.selected[i]:
			box = okStyle.Render("[x]")
		}
		if len(mod.Deps.Brew) > 0 {
			desc += mutedStyle.Render(fmt.Sprintf("  brew: %s", strings.Join(mod.Deps.Brew, ", ")))
		}
		fmt.Fprintf(&b, "%s%s %s  %s\n", cursor, box, name, desc)
	}
	b.WriteString("\n" + KeyHints("↑/↓", "move", "space", "toggle", "a", "all", "n", "none", "enter", "confirm", "q", "quit") + "\n")
	return b.String()
}
