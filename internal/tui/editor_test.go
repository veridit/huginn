package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/veridit/huginn/internal/task"
)

func TestEditorRepairsMalformedFileAndRejectsEscapes(t *testing.T) {
	m := fixture(t)
	if err := os.WriteFile(filepath.Join(m.store.Root, "broken.md"), []byte("not a task"), 0600); err != nil {
		t.Fatal(err)
	}
	m.refresh("")
	for i, v := range m.visible {
		if v.Path == "broken.md" {
			m.cursor = i
		}
	}
	t.Setenv("EDITOR", "true")
	_, cmd := key(m, "e")
	if cmd == nil {
		t.Fatal("malformed task must be editable")
	}
	for _, path := range []string{"", "../escape.md", "/escape.md", "."} {
		if _, err := m.editorPath(task.Task{Path: path}); err == nil {
			t.Errorf("accepted %q", path)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(m.store.Root, "link.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.editorPath(task.Task{Path: "link.md"}); err == nil {
		t.Fatal("accepted symlink")
	}
}
