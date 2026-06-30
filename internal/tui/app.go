package tui

import (
	"github.com/AugustDG/dotfiles/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

type View int

const (
	ViewPicker View = iota
	ViewProgress
	ViewSummary
)

type Model struct {
	view     View
	picker   PickerModel
	progress ProgressModel
	summary  SummaryModel
	results  []ModuleResult
	quitting bool
}

func NewModel(modules []config.Module) Model {
	return Model{
		view:   ViewPicker,
		picker: NewPickerModel(modules),
	}
}

func NewProgressOnlyModel() Model {
	return Model{
		view:     ViewProgress,
		progress: NewProgressModel(),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.picker.Init(), m.progress.Init())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
	}

	switch m.view {
	case ViewPicker:
		return m.updatePicker(msg)
	case ViewProgress:
		return m.updateProgress(msg)
	case ViewSummary:
		return m, nil
	}
	return m, nil
}

func (m Model) updatePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case PickerDoneMsg:
		// The picker's only job is to capture a selection. The install itself
		// runs in a SEPARATE program (see runModuleInstall) with its own
		// progress model and producer goroutine. Quitting here returns control
		// so that program can start. Advancing to ViewProgress instead would
		// render "Installing dotfiles" and spin forever, because nothing in
		// this program ever sends AllDoneMsg.
		return m, tea.Quit
	}

	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	return m, cmd
}

func (m Model) updateProgress(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ModuleResultMsg:
		m.results = append(m.results, msg.Result)
		return m, nil
	case AllDoneMsg:
		m.progress, _ = m.progress.Update(msg)
		m.view = ViewSummary
		m.summary = NewSummaryModel(m.results)
		// Auto-exit when finished, like the task runner (RunTasks) does, so the
		// command returns to the shell instead of waiting on a keypress. With
		// inline (non-altscreen) rendering the final summary frame stays on
		// screen after the program exits.
		return m, tea.Quit
	}

	var cmd tea.Cmd
	m.progress, cmd = m.progress.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	// Blank on abort (quitting) and on a confirmed selection (picker.done): in
	// the selection program the install renders in a separate program below, so
	// the picker frame should clear rather than linger.
	if m.quitting || m.picker.done {
		return ""
	}
	switch m.view {
	case ViewPicker:
		return m.picker.View()
	case ViewProgress:
		return m.progress.View()
	case ViewSummary:
		return m.progress.View() + "\n" + m.summary.View()
	}
	return ""
}

func (m *Model) AddResult(r ModuleResult) {
	m.results = append(m.results, r)
}

func (m *Model) GetProgress() *ProgressModel {
	return &m.progress
}

func (m Model) Quitting() bool {
	return m.quitting
}

// PickerDone reports whether the user confirmed a selection (pressed enter), as
// opposed to aborting with q/ctrl+c. Callers use this to tell a confirmed empty
// selection apart from an abort, which SelectedModules alone cannot express.
func (m Model) PickerDone() bool {
	return m.picker.done
}

func (m Model) SelectedModules() []config.Module {
	if m.picker.done {
		var sel []config.Module
		for i, mod := range m.picker.modules {
			if m.picker.selected[i] {
				sel = append(sel, mod)
			}
		}
		return sel
	}
	return nil
}
