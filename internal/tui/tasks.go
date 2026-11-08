package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/AugustDG/dotfiles/internal/platform"
	"github.com/AugustDG/dotfiles/internal/runner"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// ErrAborted is returned by RunTasks when the user quits (q/ctrl+c) before all
// tasks complete, so callers never mistake an interrupted run for success.
var ErrAborted = errors.New("aborted by user")

// ErrReported marks an error whose details were already shown on screen (for
// example on a failed task line), so main exits non-zero without repeating it.
var ErrReported = errors.New("already reported")

// Reported wraps err so errors.Is(err, ErrReported) holds, keeping err's
// message and identity.
func Reported(err error) error {
	if err == nil {
		return nil
	}
	return reportedError{err}
}

type reportedError struct{ err error }

func (e reportedError) Error() string   { return e.err.Error() }
func (e reportedError) Unwrap() []error { return []error{e.err, ErrReported} }

// Task is one unit of work for RunTasks.
type Task struct {
	// Group renders consecutive tasks under one heading (usually a module
	// name). When a grouped task fails, the rest of its group is skipped.
	Group string
	Title string
	// Run executes on a background goroutine and must not write to the
	// terminal. The context is cancelled if the user aborts.
	Run func(ctx context.Context) error
	// Interactive tasks may prompt (sudo, gh auth login), so they run with the
	// terminal released from the TUI.
	Interactive bool
	// Fatal stops the whole run when the task fails.
	Fatal bool
}

// Skipped marks a task as intentionally not run. RunTasks reports it as
// skipped, not failed.
func Skipped(reason string) error { return skippedError{reason} }

type skippedError struct{ reason string }

func (e skippedError) Error() string { return e.reason }

// Warning marks a task that finished with a problem that is not a failure.
func Warning(err error) error {
	if err == nil {
		return nil
	}
	return warningError{err}
}

type warningError struct{ err error }

func (e warningError) Error() string { return e.err.Error() }
func (e warningError) Unwrap() error { return e.err }

// WithHint attaches an actionable suggestion, shown under the task's error.
func WithHint(err error, hint string) error {
	if err == nil {
		return nil
	}
	return hintError{err, hint}
}

type hintError struct {
	err  error
	hint string
}

func (e hintError) Error() string { return e.err.Error() }
func (e hintError) Unwrap() error { return e.err }

// outcome classifies a task's returned error.
type outcome struct {
	level  Level
	detail string
	hint   string
}

func classify(err error) outcome {
	if err == nil {
		return outcome{level: LevelOK}
	}
	var s skippedError
	if errors.As(err, &s) {
		return outcome{level: LevelSkip, detail: s.reason}
	}
	o := outcome{level: LevelFail, detail: firstLine(err.Error())}
	var w warningError
	if errors.As(err, &w) {
		o.level = LevelWarn
	}
	var h hintError
	if errors.As(err, &h) {
		o.hint = h.hint
	}
	return o
}

// isFailure reports whether err counts as a failed task.
func isFailure(err error) bool { return err != nil && classify(err).level == LevelFail }

// FailedGroups returns, in order, each group with at least one failed task.
func FailedGroups(tasks []Task, errs []error) []string {
	var groups []string
	seen := map[string]bool{}
	for i, err := range errs {
		g := tasks[i].Group
		if err != nil && !seen[g] {
			seen[g] = true
			groups = append(groups, g)
		}
	}
	return groups
}

// FatalError returns the error of the first failed Fatal task, if any.
func FatalError(tasks []Task, errs []error) error {
	for i, err := range errs {
		if err != nil && tasks[i].Fatal {
			return fmt.Errorf("%s: %w", tasks[i].Title, err)
		}
	}
	return nil
}

// RunTasks runs tasks in order under a headline. In a terminal it shows a
// spinner and progress bar; otherwise (or with --verbose, so command output
// can stream) it prints one line per task. It returns one entry per task,
// nil unless that task failed, plus ErrAborted if the user quit early.
func RunTasks(headline string, tasks []Task) ([]error, error) {
	if len(tasks) == 0 {
		return nil, nil
	}
	fmt.Println(Title(headline))
	fmt.Println()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := &taskRun{
		tasks:        tasks,
		results:      make([]error, len(tasks)),
		outcomes:     make([]outcome, len(tasks)),
		started:      make([]bool, len(tasks)),
		failedGroups: map[string]bool{},
	}

	var err error
	if platform.IsInteractive() && !runner.Verbose {
		err = r.runTUI(ctx, cancel)
	} else {
		r.runPlain(ctx)
	}

	fmt.Println()
	fmt.Println("  " + r.summary())

	errs := make([]error, len(tasks))
	for i, res := range r.results {
		if isFailure(res) {
			errs[i] = res
		}
	}
	return errs, err
}

// taskRun holds the state of one RunTasks call. results, failedGroups and
// fatal belong to whichever goroutine is executing tasks; outcomes and started
// are display state, updated by the UI.
type taskRun struct {
	tasks        []Task
	results      []error
	outcomes     []outcome
	started      []bool
	failedGroups map[string]bool
	fatal        bool
	ran          int
}

// execute runs task i, or skips it when an earlier fatal task or a task in the
// same group failed.
func (r *taskRun) execute(ctx context.Context, i int, onStart func()) error {
	t := r.tasks[i]
	var err error
	if r.fatal || (t.Group != "" && r.failedGroups[t.Group]) {
		err = Skipped("")
	} else {
		onStart()
		err = t.Run(ctx)
		if err != nil && ctx.Err() != nil {
			err = Skipped("aborted")
		}
	}
	r.results[i] = err
	r.ran = i + 1
	if isFailure(err) {
		if t.Fatal {
			r.fatal = true
		}
		if t.Group != "" {
			r.failedGroups[t.Group] = true
		}
	}
	return err
}

func (r *taskRun) runPlain(ctx context.Context) {
	group := ""
	for i, t := range r.tasks {
		fmt.Print(groupHeader(group, t.Group))
		group = t.Group
		err := r.execute(ctx, i, func() {
			if runner.Verbose || t.Interactive {
				fmt.Println(indent(t) + activeStyle.Render(IconActive) + " " + t.Title)
			}
		})
		r.outcomes[i] = classify(err)
		fmt.Print(r.line(i, ""))
	}
}

// runTUI renders runs of non-interactive tasks with a live program each.
// Interactive tasks run between programs with the terminal attached, so a
// sudo or login prompt never fights the renderer.
func (r *taskRun) runTUI(ctx context.Context, cancel context.CancelFunc) error {
	group := ""
	for i := 0; i < len(r.tasks); {
		t := r.tasks[i]
		if t.Interactive {
			fmt.Print(groupHeader(group, t.Group))
			group = t.Group
			err := r.execute(ctx, i, func() {
				fmt.Println(indent(t) + activeStyle.Render(IconActive) + " " + t.Title)
			})
			r.outcomes[i] = classify(err)
			fmt.Print(r.line(i, ""))
			i++
			continue
		}

		end := i
		for end < len(r.tasks) && !r.tasks[end].Interactive {
			end++
		}
		quit, err := r.runSegment(ctx, cancel, i, end, group)
		if err != nil {
			return err
		}
		if quit {
			return ErrAborted
		}
		group = r.tasks[end-1].Group
		i = end
	}
	return nil
}

type (
	taskStartMsg   struct{ index int }
	segmentDoneMsg struct{}
	outcomeMsg     struct {
		index   int
		outcome outcome
	}
)

func (r *taskRun) runSegment(ctx context.Context, cancel context.CancelFunc, from, to int, group string) (bool, error) {
	program := tea.NewProgram(newSegmentModel(r, from, to, group))
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := from; i < to && ctx.Err() == nil; i++ {
			err := r.execute(ctx, i, func() { program.Send(taskStartMsg{i}) })
			program.Send(outcomeMsg{i, classify(err)})
		}
		program.Send(segmentDoneMsg{})
	}()

	final, err := program.Run()
	if err != nil || final.(segmentModel).quitting {
		// Cancel the in-flight task and wait so its cleanup (e.g. removing a
		// partial download) runs before we return.
		cancel()
		<-done
		return err == nil, err
	}
	<-done
	return false, nil
}

type segmentModel struct {
	run       *taskRun
	from, to  int
	group     string
	spinner   spinner.Model
	progress  progress.Model
	completed int
	done      bool
	quitting  bool
}

func newSegmentModel(r *taskRun, from, to int, group string) segmentModel {
	// spinner.Dot frames carry a trailing space; trim them so the spinner
	// occupies one cell like the other icons.
	frames := make([]string, len(spinner.Dot.Frames))
	for i, f := range spinner.Dot.Frames {
		frames[i] = strings.TrimSpace(f)
	}
	s := spinner.New()
	s.Spinner = spinner.Spinner{Frames: frames, FPS: spinner.Dot.FPS}
	s.Style = activeStyle
	return segmentModel{
		run:       r,
		from:      from,
		to:        to,
		group:     group,
		spinner:   s,
		progress:  progress.New(progress.WithDefaultGradient(), progress.WithWidth(40), progress.WithoutPercentage()),
		completed: from,
	}
}

func (m segmentModel) Init() tea.Cmd { return m.spinner.Tick }

func (m segmentModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		m.run.started[msg.index] = true
	case outcomeMsg:
		m.run.outcomes[msg.index] = msg.outcome
		m.run.started[msg.index] = false
		m.completed = msg.index + 1
	case segmentDoneMsg:
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

func (m segmentModel) View() string {
	var b strings.Builder
	group := m.group
	for i := m.from; i < m.to; i++ {
		t := m.run.tasks[i]
		b.WriteString(groupHeader(group, t.Group))
		group = t.Group
		icon := ""
		switch {
		case i < m.completed:
		case m.run.started[i] && m.quitting:
			icon = mutedStyle.Render(IconSkip)
		case m.run.started[i]:
			icon = m.spinner.View()
		default:
			icon = mutedStyle.Render(IconPending)
		}
		b.WriteString(m.run.line(i, icon))
	}
	if !m.done && !m.quitting {
		total := len(m.run.tasks)
		b.WriteString("\n  " + m.progress.ViewAs(float64(m.completed)/float64(total)))
		b.WriteString(mutedStyle.Render(fmt.Sprintf("  %d/%d", m.completed, total)))
		b.WriteString("    " + KeyHints("q", "abort") + "\n")
	}
	return b.String()
}

// line renders task i. An empty icon means the task has finished and its
// outcome icon is used.
func (r *taskRun) line(i int, icon string) string {
	t := r.tasks[i]
	o := r.outcomes[i]
	title := t.Title
	if icon == "" {
		icon = o.level.Icon()
		if o.level == LevelSkip {
			title = mutedStyle.Render(title)
		}
		if o.detail != "" {
			title += mutedStyle.Render(" — ") + o.level.style().Render(o.detail)
		}
	}
	s := indent(t) + icon + " " + title + "\n"
	if icon != "" && o.hint != "" {
		s += indent(t) + "  " + mutedStyle.Render(IconHint+" "+o.hint) + "\n"
	}
	return s
}

func indent(t Task) string {
	if t.Group == "" {
		return "  "
	}
	return "    "
}

func groupHeader(prev, cur string) string {
	if cur == "" || cur == prev {
		return ""
	}
	return "  " + groupStyle.Render(cur) + "\n"
}

// summary renders counts of each outcome, e.g. "3 ok · 1 failed".
func (r *taskRun) summary() string {
	counts := map[Level]int{}
	for i := 0; i < r.ran; i++ {
		counts[classify(r.results[i]).level]++
	}
	notRun := len(r.tasks) - r.ran
	var parts []string
	add := func(n int, label string, l Level) {
		if n > 0 {
			parts = append(parts, l.style().Render(fmt.Sprintf("%d %s", n, label)))
		}
	}
	add(counts[LevelOK], "ok", LevelOK)
	add(counts[LevelWarn], plural(counts[LevelWarn], "warning"), LevelWarn)
	add(counts[LevelSkip], "skipped", LevelSkip)
	add(counts[LevelFail], "failed", LevelFail)
	add(notRun, "not run", LevelSkip)
	return strings.Join(parts, mutedStyle.Render(" · "))
}

// firstLine keeps task lines to one row; the full error still reaches the
// caller.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i]) + " …"
	}
	return s
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
