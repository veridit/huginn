package cache

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirstHEADCommitPreservedMarkers(t *testing.T) {
	root := t.TempDir()
	p := "A.000001.task.md"
	put(t, root, p, fixture(1))
	g := &fakeGit{}
	c := openTest(t, root, filepath.Join(t.TempDir(), "cache.db"), g)
	defer c.Close()
	ctx := context.Background()
	if _, e := c.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	st, e := os.Stat(filepath.Join(root, p))
	if e != nil {
		t.Fatal(e)
	}
	put(t, root, p, []byte(strings.Replace(string(fixture(1)), "Task 1", "Task X", 1)))
	if e = os.Chtimes(filepath.Join(root, p), st.ModTime(), st.ModTime()); e != nil {
		t.Fatal(e)
	}
	g.head = "first-commit"
	stats, e := c.Sync(ctx)
	if e != nil || stats.Parsed != 1 {
		t.Fatal(stats, e)
	}
}
func TestAliasInvalidationCanonicalEdges(t *testing.T) {
	root := t.TempDir()
	a, b := "A.000001.task.md", "B.000002.task.md"
	put(t, root, a, fixture(1))
	body := append(fixture(2), []byte("\n## Depends on\n- old-1 an alias\n")...)
	put(t, root, b, body)
	c := openTest(t, root, filepath.Join(t.TempDir(), "cache.db"), nil)
	defer c.Close()
	ctx := context.Background()
	target := func(want string) {
		t.Helper()
		if _, e := c.Sync(ctx); e != nil {
			t.Fatal(e)
		}
		var got string
		if e := c.db.QueryRow(`SELECT "to" FROM deps`).Scan(&got); e != nil || got != want {
			t.Fatal(want, got, e)
		}
	}
	target("000001")
	put(t, root, a, []byte(strings.Replace(string(fixture(1)), "OLD-1", "NEW-1", 1)))
	target("old-1")
	put(t, root, a, fixture(1))
	target("000001")
	if e := os.Remove(filepath.Join(root, a)); e != nil {
		t.Fatal(e)
	}
	target("old-1")
}
