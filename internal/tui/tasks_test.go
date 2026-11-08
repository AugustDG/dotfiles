package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func newTestRun(tasks []Task) *taskRun {
	return &taskRun{
		tasks:        tasks,
		results:      make([]error, len(tasks)),
		outcomes:     make([]outcome, len(tasks)),
		started:      make([]bool, len(tasks)),
		failedGroups: map[string]bool{},
	}
}

func TestRunTasksSkipsRestOfFailedGroup(t *testing.T) {
	ran := map[string]bool{}
	mark := func(name string, err error) func(context.Context) error {
		return func(context.Context) error { ran[name] = true; return err }
	}
	tasks := []Task{
		{Group: "a", Title: "one", Run: mark("a1", errors.New("boom"))},
		{Group: "a", Title: "two", Run: mark("a2", nil)},
		{Group: "b", Title: "one", Run: mark("b1", nil)},
	}
	errs, err := RunTasks("test", tasks)
	if err != nil {
		t.Fatal(err)
	}
	if ran["a2"] {
		t.Error("task after a failure in the same group should be skipped")
	}
	if !ran["b1"] {
		t.Error("other groups should still run")
	}
	if errs[0] == nil || errs[1] != nil || errs[2] != nil {
		t.Errorf("only the failed task should report an error, got %v", errs)
	}
	if got := FailedGroups(tasks, errs); len(got) != 1 || got[0] != "a" {
		t.Errorf("FailedGroups = %v, want [a]", got)
	}
}

func TestRunTasksFatalStopsRun(t *testing.T) {
	later := false
	tasks := []Task{
		{Title: "brew", Run: func(context.Context) error { return errors.New("no brew") }, Fatal: true},
		{Title: "after", Run: func(context.Context) error { later = true; return nil }},
	}
	errs, _ := RunTasks("test", tasks)
	if later {
		t.Error("tasks after a fatal failure should not run")
	}
	if err := FatalError(tasks, errs); err == nil || !strings.Contains(err.Error(), "brew") {
		t.Errorf("FatalError = %v, want the brew failure", err)
	}
}

func TestWarningsAndSkipsAreNotFailures(t *testing.T) {
	tasks := []Task{
		{Title: "hook", Run: func(context.Context) error { return Warning(errors.New("hook failed")) }},
		{Title: "os", Run: func(context.Context) error { return Skipped("not supported") }},
	}
	errs, err := RunTasks("test", tasks)
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range errs {
		if e != nil {
			t.Errorf("task %d: warnings and skips should not count as failures, got %v", i, e)
		}
	}
}

func TestExecuteMarksCancelledTaskAborted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := newTestRun([]Task{{Title: "slow", Run: func(ctx context.Context) error {
		cancel()
		return ctx.Err()
	}}})
	err := r.execute(ctx, 0, func() {})
	if got := classify(err); got.level != LevelSkip || got.detail != "aborted" {
		t.Errorf("cancelled task = %+v, want skipped/aborted", got)
	}
}

func TestClassify(t *testing.T) {
	hinted := classify(WithHint(errors.New("conflict\nmore detail"), "use --adopt"))
	if hinted.level != LevelFail || hinted.hint != "use --adopt" {
		t.Errorf("hinted = %+v", hinted)
	}
	if hinted.detail != "conflict …" {
		t.Errorf("detail should be the first line, got %q", hinted.detail)
	}
	if w := classify(Warning(errors.New("x"))); w.level != LevelWarn {
		t.Errorf("warning level = %v", w.level)
	}
}

func TestReported(t *testing.T) {
	base := errors.New("boom")
	err := Reported(base)
	if !errors.Is(err, ErrReported) || !errors.Is(err, base) || err.Error() != "boom" {
		t.Errorf("Reported should keep the message and match both errors, got %v", err)
	}
	if Reported(nil) != nil {
		t.Error("Reported(nil) should be nil")
	}
}

func TestSummary(t *testing.T) {
	r := newTestRun(make([]Task, 4))
	r.results = []error{nil, Warning(errors.New("w")), errors.New("f"), nil}
	r.ran = 3
	got := r.summary()
	for _, want := range []string{"1 ok", "1 warning", "1 failed", "1 not run"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q missing %q", got, want)
		}
	}
}
