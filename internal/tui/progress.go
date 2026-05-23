package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	stepDoneStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	stepFailStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	stepRunStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	moduleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
)

type StepStartMsg struct {
	Module string
	Step   string
}

type StepDoneMsg struct {
	Module string
	Step   string
	Err    error
}

type BootstrapStepMsg struct {
	Step string
	Done bool
	Err  error
}

type ModuleResultMsg struct {
	Result ModuleResult
}

type AllDoneMsg struct{}

type completedStep struct {
	module string
	step   string
	err    error
}

type ProgressModel struct {
	spinner        spinner.Model
	completed      []completedStep
	currentModule  string
	currentStep    string
	bootstrapSteps []completedStep
	done           bool
}

func NewProgressModel() ProgressModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = stepRunStyle
	return ProgressModel{spinner: s}
}

func (m ProgressModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m ProgressModel) Update(msg tea.Msg) (ProgressModel, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case BootstrapStepMsg:
		if msg.Done {
			m.bootstrapSteps = append(m.bootstrapSteps, completedStep{
				step: msg.Step,
				err:  msg.Err,
			})
			m.currentStep = ""
		} else {
			m.currentStep = msg.Step
			m.currentModule = ""
		}
		return m, nil

	case StepStartMsg:
		m.currentModule = msg.Module
		m.currentStep = msg.Step
		return m, nil

	case StepDoneMsg:
		m.completed = append(m.completed, completedStep{
			module: msg.Module,
			step:   msg.Step,
			err:    msg.Err,
		})
		return m, nil

	case AllDoneMsg:
		m.done = true
		m.currentStep = ""
		m.currentModule = ""
		return m, nil
	}
	return m, nil
}

func (m ProgressModel) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Installing dotfiles"))
	b.WriteString("\n\n")

	for _, s := range m.bootstrapSteps {
		if s.err != nil {
			b.WriteString(fmt.Sprintf("  %s %s\n", stepFailStyle.Render("x"), s.step))
		} else {
			b.WriteString(fmt.Sprintf("  %s %s\n", stepDoneStyle.Render("✓"), s.step))
		}
	}

	lastModule := ""
	for _, s := range m.completed {
		if s.module != lastModule && s.module != "" {
			b.WriteString(fmt.Sprintf("\n  %s\n", moduleStyle.Render(s.module)))
			lastModule = s.module
		}
		if s.err != nil {
			b.WriteString(fmt.Sprintf("    %s %s — %s\n", stepFailStyle.Render("x"), s.step, s.err))
		} else {
			b.WriteString(fmt.Sprintf("    %s %s\n", stepDoneStyle.Render("✓"), s.step))
		}
	}

	if m.currentStep != "" && !m.done {
		if m.currentModule != "" && m.currentModule != lastModule {
			b.WriteString(fmt.Sprintf("\n  %s\n", moduleStyle.Render(m.currentModule)))
		}
		b.WriteString(fmt.Sprintf("    %s %s\n", m.spinner.View(), m.currentStep))
	}

	if m.done {
		b.WriteString("\n")
	}

	return b.String()
}
