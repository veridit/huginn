package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/veridit/huginn/internal/task"
)

func fixture(t *testing.T) model {
	t.Helper()
	s := &task.Store{Root: t.TempDir()}
	for _, o := range []task.NewOptions{
		{Title: "Alpha task", Goal: "Goal alpha", Acceptance: []string{"first check", "second check"}, Type: "bug", Group: "auth", Labels: []string{"urgent"}},
		{Title: "Beta task", Goal: "Goal beta", Acceptance: []string{"verify beta"}, Type: "docs"},
	} {
		if _, e := s.New(o); e != nil {
			t.Fatal(e)
		}
	}
	return newModel(s, Options{User: "tester"})
}
func key(m model, k string) (model, tea.Cmd) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	switch k {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	case "pgdown":
		msg = tea.KeyMsg{Type: tea.KeyPgDown}
	case "pgup":
		msg = tea.KeyMsg{Type: tea.KeyPgUp}
	}
	next, cmd := m.Update(msg)
	return next.(model), cmd
}
func press(m model, keys ...string) model {
	for _, k := range keys {
		m, _ = key(m, k)
	}
	return m
}
func TestNavigationViews(t *testing.T) {
	m := fixture(t)
	if !strings.Contains(m.View(), "alpha-task  Alpha task") {
		t.Fatal(m.View())
	}
	m = press(m, "j")
	if m.cursor != 1 {
		t.Fatal(m.cursor)
	}
	m = press(m, "k", "enter")
	if !strings.Contains(m.View(), "Goal alpha") {
		t.Fatal(m.View())
	}
	m = press(m, "j")
	if m.acceptance != 1 {
		t.Fatal(m.acceptance)
	}
	m = press(m, "k", "esc", "b")
	for _, s := range task.Statuses {
		if !strings.Contains(m.View(), s) {
			t.Fatal(m.View())
		}
	}
	m = press(m, "?", "pgdown", "pgup")
	if !strings.Contains(m.View(), "$EDITOR") {
		t.Fatal(m.View())
	}
	_, cmd := key(m, "q")
	if cmd == nil {
		t.Fatal("quit missing")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("not quit")
	}
}
func TestActions(t *testing.T) {
	m := fixture(t)
	first := m.visible[0].ID
	commits := 0
	m.opts.Commit = func(msg string) error {
		commits++
		if !strings.Contains(msg, first) {
			t.Error(msg)
		}
		return nil
	}
	m = press(m, "d")
	if !strings.Contains(m.message, "huginn check "+first+" 1") {
		t.Fatal(m.message)
	}
	m = press(m, "enter", " ")
	if !m.visible[0].Acceptance[0].Checked {
		t.Fatal("not checked")
	}
	m = press(m, " ")
	if m.visible[0].Acceptance[0].Checked {
		t.Fatal("not unchecked")
	}
	m = press(m, " ", "j", " ", "t")
	d, e := m.store.Get(first)
	if e != nil || d.Task.Status != "doing" || len(d.Task.Assignee) != 1 {
		t.Fatal(d, e)
	}
	m = press(m, "d")
	d, _ = m.store.Get(first)
	if d.Task.Status != "done" {
		t.Fatal(m.message)
	}
	m = press(m, "x")
	d, _ = m.store.Get(first)
	if d.Task.Status != "dropped" {
		t.Fatal(m.message)
	}
	if commits != 7 {
		t.Fatalf("commits=%d", commits)
	}
	m.opts.Commit = func(string) error { return errors.New("commit failed") }
	m = press(m, "t")
	if m.message != "commit failed" {
		t.Fatal(m.message)
	}
}
func TestMoveAndNew(t *testing.T) {
	m := fixture(t)
	id := m.visible[0].ID
	m = press(m, "J")
	if m.visible[1].ID != id {
		t.Fatal(m.visible)
	}
	m = press(m, "K")
	if m.visible[0].ID != id {
		t.Fatal(m.visible)
	}
	m = press(m, "n", "Gamma", "backspace", "a", "enter")
	if len(m.visible) != 3 {
		t.Fatal(m.message)
	}
	d, e := m.store.Get("gamma")
	if e != nil || strings.TrimSpace(d.Task.Goal) != "Gamma" || len(d.Task.Acceptance) != 1 {
		t.Fatal(d, e)
	}
	m = press(m, "n", "esc")
	if m.prompt != "" {
		t.Fatal(m.prompt)
	}
}
func TestFilters(t *testing.T) {
	for _, tc := range []struct{ k, v string }{{"s", "todo"}, {"y", "bug"}, {"g", "auth"}, {"l", "urgent"}, {"/", "Alpha"}} {
		t.Run(tc.k, func(t *testing.T) {
			m := fixture(t)
			m = press(m, tc.k, tc.v, "enter")
			want := 1
			if tc.k == "s" {
				want = 2
			}
			if len(m.visible) != want {
				t.Fatal(m.View())
			}
			m = press(m, "c")
			if len(m.visible) != 2 {
				t.Fatal(m.View())
			}
		})
	}
}
func TestMalformedAndEditorRefresh(t *testing.T) {
	m := fixture(t)
	path := filepath.Join(m.store.Root, "broken.md")
	if e := os.WriteFile(path, []byte("---\nnot: [yaml\n---\n\x1b]52;c;BAD\a"), 0600); e != nil {
		t.Fatal(e)
	}
	m = press(m, "r")
	if len(m.visible) != 3 || !strings.Contains(m.View(), "!lint") {
		t.Fatal(m.View())
	}
	m.cursor = 2
	m = press(m, "enter")
	if !strings.Contains(m.detailText(m.visible[2]), "!lint") {
		t.Fatal(m.View())
	}
	for _, k := range []string{"t", "d", "x", " ", "J", "K", "e"} {
		m, _ = key(m, k)
		_ = m.View()
	}
	m.cursor = 0
	m.detail = false
	t.Setenv("EDITOR", "true")
	_, cmd := key(m, "e")
	if cmd == nil {
		t.Fatal("editor command missing")
	}
	updated, _ := m.Update(editorMsg{})
	m = updated.(model)
	if !strings.Contains(m.message, "re-linted") {
		t.Fatal(m.message)
	}
	if strings.Contains(m.View(), "\x1b]52") {
		t.Fatal("terminal injection")
	}
}
func TestDependenciesAndNotes(t *testing.T) {
	m := fixture(t)
	a, b := m.visible[0], m.visible[1]
	d, e := m.store.Get(a.ID)
	if e != nil {
		t.Fatal(e)
	}
	raw := strings.Replace(string(d.Bytes()), "## Depends on\n", "## Depends on\n- "+b.ID+"\n", 1)
	raw = strings.Replace(raw, "## Notes\n", "## Notes\nEvidence here\n", 1)
	if e = task.AtomicWrite(filepath.Join(m.store.Root, a.Path), []byte(raw)); e != nil {
		t.Fatal(e)
	}
	m.refresh(a.Path)
	m = press(m, "enter")
	v := m.View()
	if !strings.Contains(v, "BLOCKED") || !strings.Contains(v, "Evidence here") {
		t.Fatal(v)
	}
}
func TestEmptyAndErrors(t *testing.T) {
	m := newModel(&task.Store{Root: t.TempDir()}, Options{})
	for _, k := range []string{"j", "k", "enter", " ", "t", "d", "x", "J", "K", "r", "e"} {
		m = press(m, k)
		_ = m.View()
	}
	m.store.Root = filepath.Join(t.TempDir(), "absent")
	m = press(m, "r")
	if m.message == "" {
		t.Fatal("missing IO error")
	}
	m.width = 1
	m.height = 1
	_ = m.View()
}
