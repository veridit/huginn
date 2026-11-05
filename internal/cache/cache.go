// Package cache provides the disposable, incrementally synchronized SQLite mirror.
package cache

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/veridit/huginn/internal/task"
	"io/fs"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const SchemaVersion = "1"

type Change struct{ Status, OldPath, Path string }
type Git interface {
	Head(context.Context) (string, error)
	Diff(context.Context, string, string) ([]Change, error)
}
type Marker struct {
	Path          string
	MtimeNS, Size int64
	SHA256        string
}
type record struct {
	Marker
	Task     task.Task
	Findings []task.Finding
}
type Stats struct {
	Statted, Hashed, Parsed, Renamed, Deleted int
	Rebuilt                                   bool
}
type Cache struct {
	mu      sync.Mutex
	db      *sql.DB
	Root    string
	git     Git
	head    string
	files   map[string]record
	ordered []task.Task
	rebuild bool
}

// Open takes the task worktree root and a database path, normally
// filepath.Join(mainGitDir,"huginn","cache.db"). No state is written to task files.
func Open(root, dbPath string, git Git) (*Cache, error) {
	absRoot, e := filepath.Abs(root)
	if e != nil {
		return nil, e
	}
	root = filepath.Clean(absRoot)
	if e := os.MkdirAll(filepath.Dir(dbPath), 0755); e != nil {
		return nil, e
	}
	db, e := sql.Open("sqlite", dbPath)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	c := &Cache{db: db, Root: root, git: git, files: map[string]record{}}
	fail := func(e error) (*Cache, error) { db.Close(); return nil, e }
	if _, e = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL; PRAGMA busy_timeout=5000; CREATE TABLE IF NOT EXISTS meta(key TEXT PRIMARY KEY,value TEXT NOT NULL);`); e != nil {
		return fail(e)
	}
	var version string
	e = db.QueryRow(`SELECT value FROM meta WHERE key='schema_version'`).Scan(&version)
	if e != nil && e != sql.ErrNoRows {
		return fail(e)
	}
	if version != SchemaVersion {
		c.rebuild = true
		if _, e = db.Exec(`DROP TABLE IF EXISTS tasks; DROP TABLE IF EXISTS files; DROP TABLE IF EXISTS deps; DROP TABLE IF EXISTS aliases; DELETE FROM meta;`); e != nil {
			return fail(e)
		}
	}
	_, e = db.Exec(`CREATE TABLE IF NOT EXISTS tasks(id TEXT NOT NULL,path TEXT PRIMARY KEY,"group" TEXT,rank TEXT,slug TEXT,title TEXT,status TEXT,type TEXT,priority TEXT,assignee TEXT,updated TEXT,payload TEXT NOT NULL,findings TEXT NOT NULL); CREATE INDEX IF NOT EXISTS tasks_id ON tasks(id); CREATE TABLE IF NOT EXISTS files(path TEXT PRIMARY KEY,mtime_ns INTEGER NOT NULL,size INTEGER NOT NULL,sha256 TEXT NOT NULL); CREATE TABLE IF NOT EXISTS deps("from" TEXT NOT NULL,"to" TEXT NOT NULL,line INTEGER NOT NULL,path TEXT NOT NULL); CREATE INDEX IF NOT EXISTS deps_from ON deps("from"); CREATE INDEX IF NOT EXISTS deps_to ON deps("to"); CREATE INDEX IF NOT EXISTS deps_path ON deps(path); CREATE TABLE IF NOT EXISTS aliases(alias TEXT NOT NULL,id TEXT NOT NULL,path TEXT NOT NULL); CREATE INDEX IF NOT EXISTS aliases_alias ON aliases(alias); CREATE INDEX IF NOT EXISTS aliases_path ON aliases(path);`)
	if e != nil {
		return fail(e)
	}
	rows, e := db.Query(`SELECT f.path,f.mtime_ns,f.size,f.sha256,t.payload,t.findings FROM files f JOIN tasks t ON t.path=f.path`)
	if e != nil {
		return fail(e)
	}
	for rows.Next() {
		var r record
		var payload, findings string
		if e = rows.Scan(&r.Path, &r.MtimeNS, &r.Size, &r.SHA256, &payload, &findings); e != nil {
			rows.Close()
			return fail(e)
		}
		if e = json.Unmarshal([]byte(payload), &r.Task); e != nil {
			rows.Close()
			return fail(e)
		}
		if e = json.Unmarshal([]byte(findings), &r.Findings); e != nil {
			rows.Close()
			return fail(e)
		}
		c.files[r.Path] = r
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return fail(e)
	}
	e = db.QueryRow(`SELECT value FROM meta WHERE key='head'`).Scan(&c.head)
	if e != nil && e != sql.ErrNoRows {
		return fail(e)
	}
	return c, nil
}
func (c *Cache) Close() error { c.mu.Lock(); defer c.mu.Unlock(); return c.db.Close() }
func (c *Cache) Sync(ctx context.Context) (Stats, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sync(ctx)
}
func (c *Cache) sync(ctx context.Context) (Stats, error) {
	var stats Stats
	stats.Rebuilt = c.rebuild
	head := c.head
	forced := map[string]bool{}
	renameHints := map[string]string{}
	if c.git != nil {
		var e error
		head, e = c.git.Head(ctx)
		if e != nil {
			return stats, e
		}
		if head != c.head && c.head != "" && !c.rebuild {
			changes, e := c.git.Diff(ctx, c.head, head)
			if e != nil {
				return stats, e
			}
			for _, ch := range changes {
				forced[filepath.ToSlash(ch.Path)] = true
				if strings.HasPrefix(ch.Status, "R") {
					renameHints[filepath.ToSlash(ch.Path)] = filepath.ToSlash(ch.OldPath)
				}
			}
		}
	}
	disk := make(map[string]Marker, len(c.files))
	err := filepath.WalkDir(c.Root, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && path != c.Root {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") || d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		st, e := d.Info()
		if e != nil {
			return e
		}
		rel := strings.TrimPrefix(path, c.Root+string(filepath.Separator))
		rel = filepath.ToSlash(rel)
		stats.Statted++
		disk[rel] = Marker{Path: rel, MtimeNS: st.ModTime().UnixNano(), Size: st.Size()}
		return nil
	})
	if err != nil {
		return stats, err
	}
	if !c.rebuild && len(disk) == len(c.files) && len(forced) == 0 {
		same := true
		for p, m := range disk {
			r, ok := c.files[p]
			if !ok || r.MtimeNS != m.MtimeNS || r.Size != m.Size {
				same = false
				break
			}
		}
		if same && head == c.head {
			return stats, nil
		}
	}
	// Hash only changed markers (or paths named by HEAD diff). Deleted hashes
	// identify plain filesystem renames without parsing their unchanged contents.
	deletedHashes := map[string][]string{}
	for p, r := range c.files {
		if _, ok := disk[p]; !ok {
			deletedHashes[r.SHA256] = append(deletedHashes[r.SHA256], p)
		}
	}
	next := make(map[string]record, len(disk))
	changed := map[string]record{}
	renames := map[string]string{}
	usedOld := map[string]bool{}
	for path, m := range disk {
		old, exists := c.files[path]
		if exists && !c.rebuild && !forced[path] && old.MtimeNS == m.MtimeNS && old.Size == m.Size {
			next[path] = old
			continue
		}
		b, e := os.ReadFile(filepath.Join(c.Root, path))
		if e != nil {
			return stats, e
		}
		sum := sha256.Sum256(b)
		m.SHA256 = hex.EncodeToString(sum[:])
		stats.Hashed++
		if exists && !c.rebuild && old.SHA256 == m.SHA256 {
			old.Marker = m
			next[path] = old
			changed[path] = old
			continue
		}
		oldPath := renameHints[path]
		if oldPath == "" && !exists && !c.rebuild {
			for _, p := range deletedHashes[m.SHA256] {
				if !usedOld[p] {
					oldPath = p
					break
				}
			}
		}
		prior, ok := c.files[oldPath]
		if ok && !usedOld[oldPath] && prior.SHA256 == m.SHA256 && !c.rebuild {
			r, i, slug, e := task.ParseName(path)
			if e == nil && i == prior.Task.ID {
				prior.Marker = m
				prior.Task.Path = path
				prior.Task.Group = filepath.ToSlash(filepath.Dir(path))
				if prior.Task.Group == "." {
					prior.Task.Group = ""
				}
				prior.Task.Rank = r
				prior.Task.Slug = slug
				prior.Findings = append([]task.Finding(nil), prior.Findings...)
				for n := range prior.Findings {
					prior.Findings[n].Path = path
				}
				next[path] = prior
				changed[path] = prior
				renames[oldPath] = path
				usedOld[oldPath] = true
				stats.Renamed++
				continue
			}
		}
		d := task.Parse(path, b)
		stats.Parsed++
		rec := record{Marker: m, Task: d.Task, Findings: d.Findings}
		next[path] = rec
		changed[path] = rec
	}
	removed := []string{}
	for p := range c.files {
		if _, ok := next[p]; !ok {
			removed = append(removed, p)
			if _, renamed := renames[p]; !renamed {
				stats.Deleted++
			}
		}
	}
	if len(changed) == 0 && len(removed) == 0 && head == c.head && !c.rebuild {
		return stats, nil
	}
	tx, e := c.db.BeginTx(ctx, nil)
	if e != nil {
		return stats, e
	}
	defer tx.Rollback()
	exec := func(q string, args ...any) error { _, e := tx.ExecContext(ctx, q, args...); return e }
	// Rename updates existing rows and edges rather than deleting/reinserting them.
	for old, p := range renames {
		r := next[p]
		payload, _ := json.Marshal(r.Task)
		findings, _ := json.Marshal(r.Findings)
		if e = exec(`UPDATE tasks SET path=?,"group"=?,rank=?,slug=?,payload=?,findings=? WHERE path=?`, p, r.Task.Group, r.Task.Rank, r.Task.Slug, string(payload), string(findings), old); e != nil {
			return stats, e
		}
		for _, table := range []string{"files", "deps", "aliases"} {
			if e = exec(`UPDATE `+table+` SET path=? WHERE path=?`, p, old); e != nil {
				return stats, e
			}
		}
	}
	for _, p := range removed {
		if _, ok := renames[p]; ok {
			continue
		}
		for _, table := range []string{"tasks", "files", "deps", "aliases"} {
			if e = exec(`DELETE FROM `+table+` WHERE path=?`, p); e != nil {
				return stats, e
			}
		}
	}
	taskStmt, e := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO tasks(id,path,"group",rank,slug,title,status,type,priority,assignee,updated,payload,findings) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if e != nil {
		return stats, e
	}
	defer taskStmt.Close()
	fileStmt, e := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO files(path,mtime_ns,size,sha256) VALUES(?,?,?,?)`)
	if e != nil {
		return stats, e
	}
	defer fileStmt.Close()
	renamedNew := map[string]bool{}
	for _, p := range renames {
		renamedNew[p] = true
	}
	for p, r := range changed {
		if _, e = fileStmt.ExecContext(ctx, p, r.MtimeNS, r.Size, r.SHA256); e != nil {
			return stats, e
		}
		old, existed := c.files[p]
		if renamedNew[p] || (existed && old.SHA256 == r.SHA256 && !c.rebuild) {
			continue
		}
		payload, e := json.Marshal(r.Task)
		if e != nil {
			return stats, e
		}
		findings, e := json.Marshal(r.Findings)
		if e != nil {
			return stats, e
		}
		t := r.Task
		assignee, _ := json.Marshal(t.Assignee)
		if _, e = taskStmt.ExecContext(ctx, t.ID, p, t.Group, t.Rank, t.Slug, t.Title, t.Status, t.Type, t.Priority, string(assignee), t.Updated, string(payload), string(findings)); e != nil {
			return stats, e
		}
		for _, table := range []string{"deps", "aliases"} {
			if e = exec(`DELETE FROM `+table+` WHERE path=?`, p); e != nil {
				return stats, e
			}
		}
		for _, d := range t.Dependencies {
			if e = exec(`INSERT INTO deps("from","to",line,path) VALUES(?,?,?,?)`, t.ID, d.Ref, d.Line, p); e != nil {
				return stats, e
			}
		}
		for _, a := range t.Aliases {
			if e = exec(`INSERT INTO aliases(alias,id,path) VALUES(?,?,?)`, a, t.ID, p); e != nil {
				return stats, e
			}
		}
	}
	if e = exec(`INSERT OR REPLACE INTO meta(key,value) VALUES('schema_version',?),('head',?)`, SchemaVersion, head); e != nil {
		return stats, e
	}
	if e = tx.Commit(); e != nil {
		return stats, e
	}
	c.files = next
	c.ordered = nil
	c.head = head
	c.rebuild = false
	return stats, nil
}

// List synchronizes first, then returns a detached ranked snapshot.
func (c *Cache) List(ctx context.Context) ([]task.Task, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, e := c.sync(ctx); e != nil {
		return nil, e
	}
	return c.snapshot(), nil
}
func (c *Cache) snapshot() []task.Task {
	if c.ordered == nil {
		c.ordered = make([]task.Task, 0, len(c.files))
		for _, r := range c.files {
			c.ordered = append(c.ordered, r.Task)
		}
		task.Sort(c.ordered)
	}
	out := make([]task.Task, len(c.ordered))
	for i, original := range c.ordered {
		t := original
		t.Assignee = append([]string(nil), t.Assignee...)
		t.Labels = append([]string(nil), t.Labels...)
		t.Aliases = append([]string(nil), t.Aliases...)
		t.Dependencies = append([]task.Dependency(nil), t.Dependencies...)
		t.Acceptance = append([]task.Acceptance(nil), t.Acceptance...)
		out[i] = t
	}
	return out
}
func (c *Cache) Lint(ctx context.Context) ([]task.Finding, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, e := c.sync(ctx); e != nil {
		return nil, e
	}
	docs := make([]*task.Document, 0, len(c.files))
	for _, r := range c.files {
		docs = append(docs, &task.Document{Task: r.Task, Findings: r.Findings})
	}
	return task.Lint(docs), nil
}
func (c *Cache) Markers(ctx context.Context) ([]Marker, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, e := c.sync(ctx); e != nil {
		return nil, e
	}
	var out []Marker
	for _, r := range c.files {
		out = append(out, r.Marker)
	}
	return out, nil
}
func (c *Cache) String() string { return fmt.Sprintf("huginn cache (%s)", c.Root) }
