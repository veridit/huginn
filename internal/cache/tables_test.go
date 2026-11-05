package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func tableState(t testing.TB, c *Cache) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, name := range []string{"tasks", "files", "deps", "aliases", "meta"} {
		rows, e := c.db.Query(`SELECT * FROM ` + name)
		if e != nil {
			t.Fatal(e)
		}
		cols, e := rows.Columns()
		if e != nil {
			t.Fatal(e)
		}
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for n := range vals {
				ptrs[n] = &vals[n]
			}
			if e = rows.Scan(ptrs...); e != nil {
				t.Fatal(e)
			}
			b, e := json.Marshal(vals)
			if e != nil {
				t.Fatal(e)
			}
			out[name] = append(out[name], string(b))
		}
		if e = rows.Err(); e != nil {
			t.Fatal(e)
		}
		rows.Close()
		sort.Strings(out[name])
	}
	return out
}
func TestRenameRowsAndEdges(t *testing.T) {
	root := t.TempDir()
	db := filepath.Join(t.TempDir(), "cache.db")
	for n := 1; n <= 3; n++ {
		b := fixture(n)
		if n > 1 {
			b = append(b, []byte(fmt.Sprintf("\n## Depends on\n- OLD-%d preserved note\n", n-1))...)
		}
		put(t, root, fmt.Sprintf("A.%s.task.md", testID(n)), b)
	}
	c := openTest(t, root, db, nil)
	ctx := context.Background()
	if _, e := c.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	before := tableState(t, c)
	rowids := map[string]int64{}
	old := "A.000002.task.md"
	for _, table := range []string{"tasks", "files", "deps", "aliases"} {
		var v int64
		if e := c.db.QueryRow(`SELECT rowid FROM `+table+` WHERE path=?`, old).Scan(&v); e != nil {
			t.Fatal(e)
		}
		rowids[table] = v
	}
	newPath := "B.000002.renamed.md"
	if e := os.Rename(filepath.Join(root, old), filepath.Join(root, newPath)); e != nil {
		t.Fatal(e)
	}
	st, e := c.Sync(ctx)
	if e != nil || st.Parsed != 0 || st.Renamed != 1 {
		t.Fatal(st, e)
	}
	for table, want := range rowids {
		var got int64
		if e = c.db.QueryRow(`SELECT rowid FROM `+table+` WHERE path=?`, newPath).Scan(&got); e != nil || got != want {
			t.Fatal(table, want, got, e)
		}
	}
	incremental := tableState(t, c)
	if reflect.DeepEqual(before, incremental) {
		t.Fatal("rename did not update state")
	}
	c.Close()
	if e = os.Remove(db); e != nil {
		t.Fatal(e)
	}
	c = openTest(t, root, db, nil)
	defer c.Close()
	if _, e = c.Sync(ctx); e != nil {
		t.Fatal(e)
	}
	rebuilt := tableState(t, c)
	if !reflect.DeepEqual(incremental, rebuilt) {
		t.Fatalf("all-table rebuild mismatch\n%v\n%v", incremental, rebuilt)
	}
	t.Log("rename preserves task/file/dependency/alias rowids; all five tables equal cache-deletion rebuild")
}
