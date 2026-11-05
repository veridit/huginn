package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/veridit/huginn/internal/task"
)

func TestMoveRepairsTiesAndUsesGlobalOrder(t *testing.T) {
	m := fixture(t)
	for _, v := range m.all {
		dest := filepath.Join(m.store.Root, v.Group, task.BuildName("A", v.ID, v.Slug))
		if e := os.Rename(filepath.Join(m.store.Root, v.Path), dest); e != nil {
			t.Fatal(e)
		}
	}
	m.refresh("")
	first := m.visible[0].ID
	m = press(m, "J")
	if m.visible[1].ID != first || strings.Contains(m.message, "error") {
		t.Fatal(m.View())
	}
	m = press(m, "K")
	if m.visible[0].ID != first {
		t.Fatal(m.View())
	}
	m = press(m, "/", m.visible[0].Slug, "enter", "J", "c")
	if m.visible[1].ID != first {
		t.Fatal("filtered move did not use global order", m.View())
	}
}

func TestAcceptanceSelectionClampedAcrossTasks(t *testing.T) {
	m := fixture(t)
	m = press(m, "enter", "j", "esc", "j", " ")
	if !m.visible[1].Acceptance[0].Checked {
		t.Fatal(m.View())
	}
}

func TestErrorAndTerminalInputSafety(t *testing.T) {
	m := fixture(t)
	m.opts.Commit = func(string) error { return os.ErrPermission }
	m = press(m, " ")
	if m.message != os.ErrPermission.Error() {
		t.Fatal(m.message)
	}
	t.Setenv("EDITOR", "   ")
	m, cmd := key(m, "e")
	if cmd != nil || m.message != "$EDITOR is empty" {
		t.Fatal(m.message)
	}
	next, _ := m.Update(editorMsg{os.ErrPermission})
	if next.(model).message != os.ErrPermission.Error() {
		t.Fatal("editor error lost")
	}
	next, _ = m.Update(tea.WindowSizeMsg{Width: 30, Height: 10})
	m = next.(model)
	_ = m.View()
	if strings.Contains(safe("\x1b[31mhello\a"), "\x1b") {
		t.Fatal("escape not sanitized")
	}
}
