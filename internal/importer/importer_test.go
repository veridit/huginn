package importer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/veridit/huginn/internal/task"
)

const fixture = `---
id: DEMO-001
title: >-
  Synthetic task with
  multiline title
status: To Do
assignee: []
labels: [bug]
dependencies: [DEMO-002]
ordinal: 1
custom:
  nested: 'keep me' # verbatim
---

## Description
<!-- SECTION:DESCRIPTION:BEGIN -->
Synthetic goal.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 First check
- [ ] #2 Second check
<!-- AC:END -->

## Implementation Notes
Synthetic notes.
`

func write(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}
func setup(t *testing.T) (string, *task.Store) {
	t.Helper()
	src := t.TempDir()
	dst := &task.Store{Root: t.TempDir()}
	write(t, filepath.Join(src, "one.md"), []byte(fixture))
	second := strings.ReplaceAll(fixture, "DEMO-001", "DEMO-002")
	second = strings.Replace(second, "dependencies: [DEMO-002]", "dependencies: []", 1)
	write(t, filepath.Join(src, "two.md"), []byte(second))
	return src, dst
}
func run(t *testing.T, src string, dst *task.Store, o Options) Report {
	t.Helper()
	r, e := Run(context.Background(), src, dst, o)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func hashes(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	out := map[string][32]byte{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			b, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			out[path] = sha256.Sum256(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestImportAndMerge(t *testing.T) {
	src, dst := setup(t)
	dry := run(t, src, dst, Options{DryRun: true})
	if dry.Added != 2 || len(hashes(t, dst.Root)) != 0 {
		t.Fatal(dry)
	}
	r := run(t, src, dst, Options{})
	if r.Added != 2 {
		t.Fatal(r)
	}
	before := hashes(t, dst.Root)
	r = run(t, src, dst, Options{})
	if r.Unchanged != 2 || r.Updated != 0 || !reflect.DeepEqual(before, hashes(t, dst.Root)) {
		t.Fatal("not idempotent", r)
	}
	d, e := dst.Get("DEMO-001")
	if e != nil {
		t.Fatal(e)
	}
	b := d.Bytes()
	for _, expected := range []string{"custom:\n  nested: 'keep me' # verbatim", "## Goal", "## Acceptance", "- [x] #1", "## Implementation Notes", `import_type_inference: "label: bug"`} {
		if !bytes.Contains(b, []byte(expected)) {
			t.Fatalf("missing %q", expected)
		}
	}
	if d.Task.Type != "bug" || len(d.Task.Dependencies) != 1 || d.Task.Dependencies[0].Ref == "DEMO-002" {
		t.Fatal(d.Task)
	}
	if e = d.SetField("title", "Local title"); e != nil {
		t.Fatal(e)
	}
	write(t, filepath.Join(dst.Root, d.Task.Path), d.Bytes())
	moved := filepath.Join(dst.Root, task.BuildName("z", d.Task.ID, "local-slug"))
	if e = os.Rename(filepath.Join(dst.Root, d.Task.Path), moved); e != nil {
		t.Fatal(e)
	}
	changed := strings.Replace(fixture, "Synthetic goal.", "Upstream goal.", 1)
	changed = strings.Replace(changed, "multiline title", "changed title", 1)
	write(t, filepath.Join(src, "one.md"), []byte(changed))
	r = run(t, src, dst, Options{})
	if r.Updated != 1 || r.Unchanged != 1 {
		t.Fatal(r)
	}
	d, e = dst.Get("DEMO-001")
	if e != nil {
		t.Fatal(e)
	}
	if d.Task.Title != "Local title" || !strings.Contains(d.Task.Goal, "Upstream goal.") || filepath.Join(dst.Root, d.Task.Path) != moved {
		t.Fatal(d.Task)
	}
	before = hashes(t, dst.Root)
	r = run(t, src, dst, Options{})
	if r.Unchanged != 2 || !reflect.DeepEqual(before, hashes(t, dst.Root)) {
		t.Fatal(r)
	}
}
func TestInvalidAndDangling(t *testing.T) {
	src, dst := setup(t)
	write(t, filepath.Join(src, "bad.md"), []byte("---\nid: [\n---\n"))
	write(t, filepath.Join(src, "one.md"), []byte(strings.ReplaceAll(fixture, "DEMO-002", "MISSING-123")))
	r := run(t, src, dst, Options{})
	if r.Skipped != 1 || r.Added != 2 {
		t.Fatal(r)
	}
	found := false
	for _, f := range r.Files {
		for _, finding := range f.Findings {
			found = found || strings.Contains(finding.Problem, "unresolved source dependency")
		}
	}
	if !found {
		t.Fatal("unreported dependency")
	}
}
func TestCancellation(t *testing.T) {
	src, dst := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, src, dst, Options{}); err != context.Canceled {
		t.Fatal(err)
	}
	if len(hashes(t, dst.Root)) != 0 {
		t.Fatal("wrote after cancellation")
	}
}

func TestAliasDeletionAndCollision(t *testing.T) {
	src, dst := setup(t)
	occupied := stableID("DEMO-001", 0)
	write(t, filepath.Join(dst.Root, task.BuildName("", occupied, "unrelated")), []byte("---\nid: "+occupied+"\ntitle: Unrelated\nstatus: todo\n---\n## Goal\nUnrelated.\n## Acceptance\n- [ ] Check\n"))
	r := run(t, src, dst, Options{})
	if r.Added != 2 {
		t.Fatal(r)
	}
	d, err := dst.Get("DEMO-001")
	if err != nil {
		t.Fatal(err)
	}
	if d.Task.ID == occupied {
		t.Fatal("collision was not checked")
	}
	id := d.Task.ID
	if err = d.SetField("aliases", []string{}); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dst.Root, d.Task.Path), d.Bytes())
	r = run(t, src, dst, Options{})
	if r.Added != 0 || r.Unchanged != 2 {
		t.Fatal(r)
	}
	d, err = dst.Get(id)
	if err != nil || len(d.Task.Aliases) != 0 {
		t.Fatal("local alias edit lost", err)
	}
}

func TestMergeUnits(t *testing.T) {
	base := []byte("---\nid: 000001\nunknown:\n  x: old # keep\nstatus: todo\n---\n\n## Goal\nOld.\n\n## Acceptance\n- [ ] Check\n\n## Notes\nOld note.\n")
	local := bytes.Replace(base, []byte("Old note."), []byte("Local note."), 1)
	incoming := bytes.Replace(base, []byte("Old."), []byte("New."), 1)
	incoming = bytes.Replace(incoming, []byte("status: todo"), []byte("status: doing\nnew_key: 'raw value'"), 1)
	incoming = bytes.Replace(incoming, []byte("Old note."), []byte("Upstream note."), 1)
	result, err := merge(base, local, incoming)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"New.", "Local note.", "status: doing", "new_key: 'raw value'", "unknown:\n  x: old # keep"} {
		if !bytes.Contains(result, []byte(want)) {
			t.Fatalf("missing %q in %s", want, result)
		}
	}
	if _, _, err = front(result); err != nil {
		t.Fatal(err)
	}
	again, err := merge(incoming, result, incoming)
	if err != nil || !bytes.Equal(again, result) {
		t.Fatal("merge not idempotent", err)
	}
}

func TestUnsafePaths(t *testing.T) {
	src, dst := setup(t)
	if err := os.Symlink(t.TempDir(), filepath.Join(dst.Root, ".backlog-import")); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), src, dst, Options{}); err == nil {
		t.Fatal("accepted symlinked baseline directory")
	}
	if _, err := Run(context.Background(), src, &task.Store{Root: src}, Options{}); err == nil {
		t.Fatal("accepted overlapping roots")
	}
}

func TestMetadataDoesNotClobberUnknownFields(t *testing.T) {
	raw := strings.Replace(fixture, "status: To Do", "status: Waiting\naliases: [LEGACY-7]\ntype: enhancement\nimport_type_inference: 'source custom' # untouched\nbacklog_type: 'source original'\ncreated: 'keep local date'\ncreated_date: '2026-01-02'", 1)
	fields, _, err := front([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	converted, err := convert(source{raw: []byte(raw), fields: fields, alias: "DEMO-001", title: "Synthetic"}, "000001", map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"import_type_inference: 'source custom' # untouched", "backlog_type: 'source original'", "created: 'keep local date'", `backlog_type_2: "enhancement"`, `import_type_inference_2: "label: bug"`} {
		if !bytes.Contains(converted, []byte(want)) {
			t.Fatalf("missing preserved field %q", want)
		}
	}
	d := task.Parse("000001.synthetic.md", converted)
	if d.Task.Status != "Waiting" || len(d.Task.Aliases) != 2 {
		t.Fatal(d.Task)
	}
}

// withoutOrigin reconstructs the pre-D21 representation exactly: origin is
// the only new generated span. Apply it to both task and durable baseline.
func withoutOrigin(t *testing.T, raw []byte) []byte {
	t.Helper()
	parts, err := units(raw)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	for _, part := range parts {
		if part.key != "field:origin" {
			out.WriteString(part.raw)
		}
	}
	return []byte(out.String())
}
func makeLegacy(t *testing.T, dst *task.Store) {
	t.Helper()
	items, err := dst.Tasks()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		path := filepath.Join(dst.Root, item.Path)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		write(t, path, withoutOrigin(t, raw))
		state := filepath.Join(dst.Root, ".backlog-import", item.ID+".json")
		raw, err = os.ReadFile(state)
		if err != nil {
			t.Fatal(err)
		}
		var base []byte
		if err = json.Unmarshal(raw, &base); err != nil {
			t.Fatal(err)
		}
		raw, err = json.Marshal(withoutOrigin(t, base))
		if err != nil {
			t.Fatal(err)
		}
		write(t, state, raw)
	}
}
func TestOriginLegacyBackfill(t *testing.T) {
	src, dst := setup(t)
	run(t, src, dst, Options{})
	makeLegacy(t, dst)
	before := hashes(t, dst.Root)
	r := run(t, src, dst, Options{DryRun: true})
	if r.Updated != 2 || !reflect.DeepEqual(before, hashes(t, dst.Root)) {
		t.Fatal("backfill dry run", r)
	}
	r = run(t, src, dst, Options{})
	if r.Added != 0 || r.Updated != 2 {
		t.Fatal("backfill", r)
	}
	before = hashes(t, dst.Root)
	r = run(t, src, dst, Options{})
	if r.Updated != 0 || r.Unchanged != 2 || !reflect.DeepEqual(before, hashes(t, dst.Root)) {
		t.Fatal("backfill unstable", r)
	}
}
func TestOriginFirstAndLocalRelink(t *testing.T) {
	src, dst := setup(t)
	run(t, src, dst, Options{})
	one, err := dst.Get("DEMO-001")
	if err != nil {
		t.Fatal(err)
	}
	two, err := dst.Get("DEMO-002")
	if err != nil {
		t.Fatal(err)
	}
	// Swap the explicit links, leaving aliases and baselines unchanged. Origins
	// must win, and imported units must not reset these human edits.
	if err = one.SetField("origin", "backlog:DEMO-002"); err != nil {
		t.Fatal(err)
	}
	if err = two.SetField("origin", "backlog:DEMO-001"); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dst.Root, one.Task.Path), one.Bytes())
	write(t, filepath.Join(dst.Root, two.Task.Path), two.Bytes())
	r := run(t, src, dst, Options{})
	if r.Added != 0 {
		t.Fatal("ignored explicit relink", r)
	}
	for _, f := range r.Files {
		if strings.HasSuffix(f.Source, "one.md") && f.ID != two.Task.ID {
			t.Fatal("origin did not win over alias", f)
		}
		if strings.HasSuffix(f.Source, "two.md") && f.ID != one.Task.ID {
			t.Fatal("origin did not win over baseline", f)
		}
	}
	d, err := dst.Get(one.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	fields, _, err := front(d.Bytes())
	if err != nil || str(fields["origin"]) != "backlog:DEMO-002" {
		t.Fatal("local origin edit clobbered", fields, err)
	}
	before := hashes(t, dst.Root)
	r = run(t, src, dst, Options{})
	if r.Unchanged != 2 || !reflect.DeepEqual(before, hashes(t, dst.Root)) {
		t.Fatal("relink unstable", r)
	}
}
func TestDuplicateOrigins(t *testing.T) {
	src, dst := setup(t)
	run(t, src, dst, Options{})
	d, err := dst.Get("DEMO-002")
	if err != nil {
		t.Fatal(err)
	}
	if err = d.SetField("origin", "backlog:DEMO-001"); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dst.Root, d.Task.Path), d.Bytes())
	before := hashes(t, dst.Root)
	r, err := Run(context.Background(), src, dst, Options{})
	if err == nil || !strings.Contains(err.Error(), "duplicate destination origin") {
		t.Fatal("duplicate accepted", err)
	}
	n := 0
	for _, f := range r.Files {
		for _, finding := range f.Findings {
			if finding.Problem == "duplicate origin backlog:DEMO-001" {
				n++
			}
		}
	}
	if n != 2 || !reflect.DeepEqual(before, hashes(t, dst.Root)) {
		t.Fatal("missing findings or mutated ambiguous destination", r)
	}
}

// Real task content is only read into a test-time copy outside the repository.
func TestRealStatbus(t *testing.T) {
	home, e := os.UserHomeDir()
	if e != nil {
		t.Skip(e)
	}
	real := filepath.Join(home, "ssb/statbus/.backlog/tasks")
	entries, e := os.ReadDir(real)
	if os.IsNotExist(e) {
		t.Skip("real statbus directory absent")
	}
	if e != nil {
		t.Fatal(e)
	}
	src := t.TempDir()
	dst := &task.Store{Root: t.TempDir()}
	type ordered struct {
		alias   string
		ordinal float64
		ranked  bool
	}
	var want []ordered
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		b, e := os.ReadFile(filepath.Join(real, entry.Name()))
		if e != nil {
			t.Fatal(e)
		}
		write(t, filepath.Join(src, entry.Name()), b)
		fields, _, e := front(b)
		if e != nil {
			t.Fatal(e)
		}
		s := ordered{alias: canonical(str(fields["id"]))}
		if v, ok := fields["ordinal"]; ok {
			_, e = fmt.Sscan(str(v), &s.ordinal)
			s.ranked = e == nil
		}
		want = append(want, s)
	}
	r := run(t, src, dst, Options{})
	if r.Added != len(want) || r.Skipped != 0 {
		t.Fatalf("added=%d skipped=%d", r.Added, r.Skipped)
	}
	tasks, e := dst.Tasks()
	if e != nil {
		t.Fatal(e)
	}
	docs := []*task.Document{}
	problems := map[string]int{}
	for _, tt := range tasks {
		d, e := dst.Get(tt.ID)
		if e != nil {
			t.Fatal(e)
		}
		docs = append(docs, d)
		fields, _, e := front(d.Bytes())
		if e != nil || str(fields["origin"]) != "backlog:"+tt.Aliases[0] {
			t.Fatalf("missing origin for %s: %v", tt.ID, e)
		}
		for _, a := range tt.Aliases {
			resolved, e := task.Resolve(tasks, a)
			if e != nil || resolved.ID != tt.ID {
				t.Fatal("alias", a, e)
			}
		}
		for _, dep := range tt.Dependencies {
			if _, e := task.ResolveDependency(tasks, dep.Ref); e != nil {
				reported := false
				for _, fr := range r.Files {
					if fr.ID == tt.ID {
						for _, f := range fr.Findings {
							reported = reported || strings.HasPrefix(f.Problem, "unresolved source dependency:")
						}
					}
				}
				if !reported {
					t.Fatalf("unreported dependency %s", dep.Ref)
				}
			}
		}
	}
	for _, f := range task.Lint(docs) {
		problems[f.Problem]++
		for _, d := range docs {
			if d.Task.Path == f.Path {
				t.Logf("source problem: %s: %s", strings.Join(d.Task.Aliases, ","), f.Problem)
				break
			}
		}
	}
	// Only intrinsic source defects are allowed, never importer name/YAML/id defects.
	for problem, n := range problems {
		if !strings.Contains(problem, "Goal") && !strings.Contains(problem, "Acceptance") && !strings.Contains(problem, "unchecked") && !strings.Contains(problem, "dangling") && !strings.Contains(problem, "cycle") {
			t.Errorf("unexpected lint: %s (%d)", problem, n)
		}
	}
	keys := []string{}
	for k := range problems {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Logf("source lint: %d x %s", problems[k], k)
	}
	sort.Slice(want, func(i, j int) bool {
		a, b := want[i], want[j]
		if a.ranked != b.ranked {
			return a.ranked
		}
		if a.ordinal != b.ordinal {
			return a.ordinal < b.ordinal
		}
		return a.alias < b.alias
	})
	task.Sort(tasks)
	// Unranked tasks have no source order. Ranked ordinal ties use source-id order.
	for i, w := range want {
		if !w.ranked {
			break
		}
		if len(tasks[i].Aliases) == 0 || tasks[i].Aliases[0] != w.alias {
			t.Fatalf("order %d: got %v want %s", i, tasks[i].Aliases, w.alias)
		}
	}
	before := hashes(t, dst.Root)
	r = run(t, src, dst, Options{})
	if r.Added != 0 || r.Updated != 0 || r.Unchanged != len(want) || !reflect.DeepEqual(before, hashes(t, dst.Root)) {
		t.Fatalf("second run added=%d updated=%d unchanged=%d byte-identical=%t", r.Added, r.Updated, r.Unchanged, reflect.DeepEqual(before, hashes(t, dst.Root)))
	}
	t.Logf("%d imported; every alias resolves; every dependency resolves or is reported; ordinal order preserved", len(want))
	t.Logf("second run: added 0 updated 0 unchanged %d; all destination SHA-256 hashes byte-identical", len(want))
	makeLegacy(t, dst)
	r = run(t, src, dst, Options{})
	if r.Added != 0 || r.Updated != len(want) || r.Unchanged != 0 {
		t.Fatal("legacy real backfill", r)
	}
	before = hashes(t, dst.Root)
	r = run(t, src, dst, Options{})
	if r.Added != 0 || r.Updated != 0 || r.Unchanged != len(want) || !reflect.DeepEqual(before, hashes(t, dst.Root)) {
		t.Fatal("legacy real backfill not stable", r)
	}
	t.Logf("D21: origin set on all %d fresh tasks; legacy backfill updated %d once; next run unchanged %d byte-identical", len(want), len(want), len(want))
}
func FuzzParsing(f *testing.F) {
	f.Add([]byte(fixture))
	f.Add([]byte("---\n{}\n---\n"))
	f.Add([]byte("garbage"))
	f.Fuzz(func(t *testing.T, b []byte) {
		fields, _, err := front(b)
		if err != nil {
			return
		}
		_, _ = units(b)
		s := source{alias: canonical(str(fields["id"])), fields: fields, raw: b, title: str(fields["title"])}
		converted, e := convert(s, "000001", map[string]string{})
		if e == nil {
			_, _ = merge(converted, converted, converted)
		}
	})
}
