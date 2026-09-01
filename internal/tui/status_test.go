package tui

import (
	"strings"
	"testing"

	"github.com/AugustDG/dotfiles/internal/config"
)

func TestRenderStatusTableShowsDirtyModule(t *testing.T) {
	out := RenderStatusTable([]ModuleStatus{
		{
			Module:     config.Module{Name: "atuin", Description: "atuin config", IsStowed: true},
			Dirty:      true,
			Compatible: true,
		},
		{
			Module:     config.Module{Name: "zsh", Description: "zsh config", IsStowed: true},
			Compatible: true,
		},
	})

	if !strings.Contains(out, "Changes") {
		t.Fatalf("expected Changes column in output:\n%s", out)
	}

	lines := strings.Split(out, "\n")
	assertRowContains := func(module, state string) {
		t.Helper()
		for _, line := range lines {
			if strings.Contains(line, module) {
				if !strings.Contains(line, state) {
					t.Errorf("expected %q row to contain %q: %s", module, state, line)
				}
				return
			}
		}
		t.Errorf("missing %q row in output:\n%s", module, out)
	}

	assertRowContains("atuin", "dirty")
	assertRowContains("zsh", "clean")
}
