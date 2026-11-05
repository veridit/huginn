package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/veridit/huginn/internal/task"
)

func TestTransactionalRaceRefreshesWinner(t *testing.T) {
	m := fixture(t)
	m.opts.Take = func(ref string) (task.Result, error) {
		if _, err := m.store.Take(ref, "alice"); err != nil {
			t.Fatal(err)
		}
		return task.Result{}, errors.New("taken by alice")
	}
	m = press(m, "t")
	if m.message != "taken by alice" || !contains(m.visible[0].Assignee, "alice") || m.visible[0].Status != "doing" {
		t.Fatal(m.View())
	}
}

func TestTransactionalTakeOwnsCommit(t *testing.T) {
	m := fixture(t)
	id := m.visible[0].ID
	calls, commits := 0, 0
	m.opts.Take = func(ref string) (task.Result, error) {
		calls++
		if ref != id {
			t.Errorf("ref=%q", ref)
		}
		return m.store.Take(ref, "remote-user")
	}
	m.opts.Commit = func(string) error { commits++; return nil }
	m = press(m, "t")
	if calls != 1 || commits != 0 {
		t.Fatalf("take=%d commits=%d", calls, commits)
	}
	d, err := m.store.Get(id)
	if err != nil || d.Task.Status != "doing" || !contains(d.Task.Assignee, "remote-user") {
		t.Fatal(d, err)
	}
	m = press(m, "x")
	if commits != 1 {
		t.Fatal("ordinary mutation must still commit")
	}
}
func TestTransactionalTakeErrorsStayInline(t *testing.T) {
	m := fixture(t)
	m.opts.Take = func(string) (task.Result, error) { return task.Result{}, errors.New("taken by alice") }
	m.opts.Commit = func(string) error { t.Fatal("must not commit failed transaction"); return nil }
	m = press(m, "t")
	if !strings.Contains(m.View(), "taken by alice") {
		t.Fatal(m.View())
	}
	d, err := m.store.Get(m.visible[0].ID)
	if err != nil || d.Task.Status != "todo" {
		t.Fatal(d, err)
	}
}
