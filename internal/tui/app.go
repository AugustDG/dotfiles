package tui

import (
	"github.com/AugustDG/dotfiles/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

type View int

const (
	ViewPicker   View = iota
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
		if m.view == ViewSummary && msg.String() == "enter" {
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
		m.view = ViewProgress
		m.progress = NewProgressModel()
		return m, m.progress.Init()
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
		return m, nil
	}

	var cmd tea.Cmd
	m.progress, cmd = m.progress.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	if m.quitting {
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
