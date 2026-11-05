package cache

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/veridit/huginn/internal/task"
)

func TestDependencyOperationsRefreshCanonicalEdges(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	put(t, root, "a.000001.task.md", fixture(1))
	put(t, root, "b.000002.task.md", fixture(2))
	c := openTest(t, root, filepath.Join(t.TempDir(), "cache.db"), nil)
	defer c.Close()
	s := &task.Store{Root: root, Refresh: func() ([]task.Task, error) { return c.List(ctx) }}
	r, err := s.AddDependencies("000002", []string{"OLD-1"})
	if err != nil || !r.Changed {
		t.Fatal(r, err)
	}
	var from, to, ref string
	if err := c.db.QueryRowContext(ctx, `SELECT "from", "to", ref FROM deps`).Scan(&from, &to, &ref); err != nil {
		t.Fatal(err)
	}
	if from != "000002" || to != "000001" || ref != "000001" {
		t.Fatal("cache did not receive canonical edge", from, to, ref)
	}
	if _, err := s.AddDependencies("000001", []string{"000002"}); err == nil {
		t.Fatal("cycle accepted through cache-backed Store")
	}
	r, err = s.RemoveDependencies("000002", []string{"OLD-1"})
	if err != nil || !r.Changed {
		t.Fatal(r, err)
	}
	var count int
	if err := c.db.QueryRowContext(ctx, `SELECT count(*) FROM deps`).Scan(&count); err != nil || count != 0 {
		t.Fatal("cache retained removed edge", count, err)
	}
	findings, err := c.Lint(ctx)
	if err != nil || len(findings) != 0 {
		t.Fatal(findings, err)
	}
}
