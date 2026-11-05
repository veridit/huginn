package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/veridit/huginn/internal/task"
)

func FuzzMalformedTaskView(f *testing.F) {
	for _, seed := range [][]byte{nil, []byte("---\nid: [broken\n---"), []byte("\xff\x1b]52;c;bad\a"), []byte("## Goal\nmissing metadata\n")} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 65536 {
			t.Skip()
		}
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "broken.md"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		m := newModel(&task.Store{Root: root}, Options{User: "fuzzer"})
		for _, k := range []string{"j", "k", "enter", " ", "t", "d", "x", "J", "K", "r", "b", "?", "pgdown", "pgup", "q"} {
			m = press(m, k)
			_ = m.View()
		}
	})
}
