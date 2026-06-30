package tui

import (
	"fmt"
	"strings"

	"github.com/AugustDG/dotfiles/internal/platform"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// Task is one unit of work for the task runner. Run is executed on a background
// goroutine; it must not touch the terminal directly.
type Task struct {
	Title string
	Run   func() error
}

type taskState int

const (
	taskPending taskState = iota
	taskRunning
	taskOK
	taskFail
)

type (
	taskStartMsg struct{ index int }
	taskDoneMsg  struct {
		index int
		err   error
	}
	tasksAllDoneMsg struct{}
)

type taskRunnerModel struct {
	headline string
	tasks    []Task
	state    []taskState
	errs     []error

	progress  progress.Model
	spinner   spinner.Model
	completed int
	done      bool
	quitting  bool
}

func newTaskRunnerModel(headline string, tasks []Task) taskRunnerModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = stepRunStyle

	return taskRunnerModel{
		headline: headline,
		tasks:    tasks,
		state:    make([]taskState, len(tasks)),
		errs:     make([]error, len(tasks)),
		progress: progress.New(progress.WithDefaultGradient(), progress.WithWidth(40)),
		spinner:  s,
	}
}

func (m taskRunnerModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m taskRunnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if s := msg.String(); s == "q" || s == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case taskStartMsg:
		m.state[msg.index] = taskRunning
		return m, nil
	case taskDoneMsg:
		if msg.err != nil {
			m.state[msg.index] = taskFail
			m.errs[msg.index] = msg.err
		} else {
			m.state[msg.index] = taskOK
		}
		m.completed++
		return m, nil
	case tasksAllDoneMsg:
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

func (m taskRunnerModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render(m.headline))
	b.WriteString("\n\n")

	percent := 0.0
	if len(m.tasks) > 0 {
		percent = float64(m.completed) / float64(len(m.tasks))
	}
	b.WriteString("  " + m.progress.ViewAs(percent))
	b.WriteString(hintStyle.Render(fmt.Sprintf("  %d/%d", m.completed, len(m.tasks))))
	b.WriteString("\n\n")

	for i, t := range m.tasks {
		var icon string
		switch m.state[i] {
		case taskRunning:
			icon = m.spinner.View()
		case taskOK:
			icon = stepDoneStyle.Render("✓")
		case taskFail:
			icon = stepFailStyle.Render("x")
		default:
			icon = hintStyle.Render("·")
		}
		line := fmt.Sprintf("  %s %s", icon, t.Title)
		if m.errs[i] != nil {
			line += " — " + stepFailStyle.Render(m.errs[i].Error())
		}
		b.WriteString(line + "\n")
	}

	return b.String()
}

// RunTasks executes tasks sequentially, displaying a progress bar and spinner
// when attached to a terminal, or plain line output otherwise. It returns one
// error per task (nil when that task succeeded) plus a fatal error if the
// terminal program itself failed.
func RunTasks(headline string, tasks []Task) ([]error, error) {
	if len(tasks) == 0 {
		return nil, nil
	}
	if !platform.IsInteractive() {
		return runTasksPlain(headline, tasks), nil
	}

	program := tea.NewProgram(newTaskRunnerModel(headline, tasks))
	go func() {
		for i, t := range tasks {
			program.Send(taskStartMsg{index: i})
			err := t.Run()
			program.Send(taskDoneMsg{index: i, err: err})
		}
		program.Send(tasksAllDoneMsg{})
	}()

	final, err := program.Run()
	if err != nil {
		return nil, err
	}
	return final.(taskRunnerModel).errs, nil
}

func runTasksPlain(headline string, tasks []Task) []error {
	fmt.Println(headline)
	errs := make([]error, len(tasks))
	for i, t := range tasks {
		fmt.Printf("  %s... ", t.Title)
		err := t.Run()
		errs[i] = err
		if err != nil {
			fmt.Printf("failed: %s\n", err)
		} else {
			fmt.Println("done")
		}
	}
	return errs
}
