package task

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const sample = "---\nid: k7m2qx\ntitle: Recall gate\nstatus: todo\ntype: bug\nlabels: [recall, bug]\naliases: [STATBUS-093]\nx-opaque: {wow: 'yes'} # keep\n---\n\n## Goal\nWorks.\n\n## Depends on\n\n## Acceptance\n- [ ] observable one\n- [x] observable two\n\n## Notes\nExisting.\n\n## Weird\n\x00\xff preserved\n"

func TestRoundTripCorpus(t *testing.T) {
	corpus := [][]byte{nil, {}, []byte("garbage"), []byte(sample), bytes.ReplaceAll([]byte(sample), []byte("\n"), []byte("\r\n")), []byte("---\nid:"), []byte("---\nstatus: []\n---"), {0, 255, 128, 10}, []byte("---\nunknown: |\n  nested: yes\n---\n## Free\nanything"), []byte(strings.TrimSuffix(sample, "\n"))}
	for n, b := range corpus {
		d := Parse("a0.k7m2qx.recall-gate.md", b)
		if !bytes.Equal(d.Bytes(), b) {
			t.Fatalf("corpus %d changed", n)
		}
	}
	t.Logf("round-trip byte-identical: %d weird-file corpus cases", len(corpus))
}
func FuzzParse(f *testing.F) {
	for _, b := range [][]byte{[]byte(sample), {}, []byte("---\nid:\n---"), {0, 255, 128}} {
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		d := Parse("broken.md", b)
		if !bytes.Equal(b, d.Bytes()) {
			t.Fatal("roundtrip changed bytes")
		}
	})
}
func TestDocumentEditsPreserveOpaque(t *testing.T) {
	d := Parse("a0.k7m2qx.recall-gate.md", []byte(sample))
	if len(d.Findings) != 0 {
		t.Fatal(d.Findings)
	}
	if err := d.SetField("title", "A new title"); err != nil {
		t.Fatal(err)
	}
	if d.Task.Title != "A new title" {
		t.Fatal(d.Task)
	}
	for _, s := range []string{"x-opaque: {wow: 'yes'} # keep\n", "## Weird\n\x00\xff preserved\n"} {
		if !bytes.Contains(d.Bytes(), []byte(s)) {
			t.Fatalf("lost unknown %q", s)
		}
	}
	if err := d.Check(1); err != nil {
		t.Fatal(err)
	}
	d.Note("evidence")
	if !d.Task.Acceptance[0].Checked || !bytes.Contains(d.Bytes(), []byte("evidence\n## Weird")) {
		t.Fatal(string(d.Bytes()))
	}
	before := d.Bytes()
	if err := d.Check(1); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, d.Bytes()) {
		t.Fatal("repeat check changed document")
	}
}
func TestNames(t *testing.T) {
	r, i, s, e := ParseName("auth/a0.k7m2qx.recall-gate.md")
	if e != nil || r != "a0" || i != "k7m2qx" || s != "recall-gate" {
		t.Fatal(r, i, s, e)
	}
	if BuildName(r, i, s) != "a0.k7m2qx.recall-gate.md" {
		t.Fatal("name")
	}
	if Slug("One TWO, three four five six seven") != "one-two-three-four-five-six" {
		t.Fatal("slug")
	}
	d := Parse("a0.p3rt9a.wrong.md", []byte(sample))
	if d.Task.ID != "k7m2qx" || len(d.Findings) == 0 {
		t.Fatal(d.Task, d.Findings)
	}
}
func TestResolveAndErrors(t *testing.T) {
	ts := []Task{{ID: "k7m2qx", Slug: "same", Aliases: []string{"STATBUS-093"}}, {ID: "k7m2qa", Slug: "same"}}
	for _, ref := range []string{"K7M2QX", "statbus-093"} {
		v, e := Resolve(ts, ref)
		if e != nil || v.ID != "k7m2qx" {
			t.Fatal(ref, v, e)
		}
	}
	for _, ref := range []string{"same", "k7m"} {
		_, e := Resolve(ts, ref)
		var te *Error
		if !errors.As(e, &te) || te.Kind != "ambiguous" || len(te.Matches) != 2 {
			t.Fatal(e)
		}
	}
	e := Invalid("status", "doin", Statuses)
	if e.Closest != "doing" || e.ExitCode() != 2 {
		t.Fatal(e)
	}
	if _, e := ResolveDependency(ts, "same"); e == nil {
		t.Fatal("slug accepted as dep")
	}
}
func newTest(t *testing.T, s *Store, title string, deps ...string) Task {
	t.Helper()
	r, e := s.New(NewOptions{Title: title, Goal: "observable outcome", Acceptance: []string{"passes"}, Deps: deps, Type: "bug", Labels: []string{"core"}})
	if e != nil {
		t.Fatal(e)
	}
	return r.Task
}
func hashes(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	out := map[string][32]byte{}
	if e := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			b, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			rel, _ := filepath.Rel(root, p)
			out[rel] = sha256.Sum256(b)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	return out
}
func TestOperationsIsolationAndLifecycle(t *testing.T) {
	s := &Store{Root: t.TempDir()}
	a := newTest(t, s, "First task")
	b := newTest(t, s, "Second task", a.ID)
	c := newTest(t, s, "Third task", b.ID)
	initial := hashes(t, s.Root)
	for _, op := range []func() (Result, error){func() (Result, error) { return s.Move(b.ID, MoveOptions{Top: true}) }, func() (Result, error) { return s.Reslug(b.ID, "new-slug") }, func() (Result, error) { return s.MoveGroup(b.ID, "auth/nested") }} {
		r, e := op()
		if e != nil {
			t.Fatal(e)
		}
		now := hashes(t, s.Root)
		if now[a.Path] != initial[a.Path] || now[c.Path] != initial[c.Path] {
			t.Fatal("rename changed another task")
		}
		bb, e := os.ReadFile(filepath.Join(s.Root, r.Task.Path))
		if e != nil {
			t.Fatal(e)
		}
		if sha256.Sum256(bb) != initial[b.Path] {
			t.Fatal("rename changed content")
		}
	}
	if _, e := s.Take(b.ID, "alice"); e == nil {
		t.Fatal("blocked take accepted")
	} else {
		var te *Error
		if !errors.As(e, &te) || te.ExitCode() != 3 || te.Dependencies[0].ID != a.ID {
			t.Fatal(e)
		}
	}
	r, e := s.Take(a.ID, "alice")
	if e != nil || !r.Changed {
		t.Fatal(r, e)
	}
	r, e = s.Take(a.ID, "alice")
	if e != nil || r.Changed {
		t.Fatal("take repeat", r, e)
	}
	if _, e = s.Take(a.ID, "bob"); e == nil {
		t.Fatal("stole task")
	}
	if _, e = s.Check(a.ID, 1); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Done(a.ID); e != nil {
		t.Fatal(e)
	}
	r, e = s.Done(a.ID)
	if e != nil || r.Changed {
		t.Fatal("done repeat", r, e)
	}
	if _, e = s.Take(b.ID, "alice"); e != nil {
		t.Fatal(e)
	}
	r, e = s.Done(b.ID)
	if e != nil || len(r.Warnings) != 1 {
		t.Fatal(r, e)
	}
	if _, e = s.Note(c.ID, "evidence"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Set(c.ID, "title", "Renamed title"); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(s.Root, c.Path)); e != nil {
		t.Fatal("title changed slug", e)
	}
	r, e = s.Drop(c.ID)
	if e != nil || !r.Changed {
		t.Fatal(r, e)
	}
	r, e = s.Drop(c.ID)
	if e != nil || r.Changed {
		t.Fatal(r, e)
	}
	t.Log("move/re-slug/rerank/mv hashes: all other files and moved contents unchanged")
}
func TestNextFilters(t *testing.T) {
	ts := []Task{{ID: "000001", Rank: "Z", Status: "todo", Type: "bug", Priority: "high", Labels: []string{"x"}, Group: "auth"}, {ID: "000002", Rank: "A", Status: "todo", Type: "chore"}, {ID: "000003", Rank: "B", Status: "todo", Type: "bug", Assignee: []string{"alice"}}, {ID: "000004", Rank: "0", Status: "todo", Dependencies: []Dependency{{Ref: "000002"}}}}
	for _, tc := range []struct {
		f  Filter
		id string
	}{{Filter{}, "000002"}, {Filter{Type: "bug", Priority: "high", Label: "x", Group: "auth"}, "000001"}, {Filter{Mine: "alice"}, "000003"}} {
		r, e := Next(ts, tc.f)
		if e != nil || r.Task.ID != tc.id {
			t.Fatal(tc, r, e)
		}
	}
	_, e := Next(ts, Filter{Label: "missing"})
	var te *Error
	if !errors.As(e, &te) || te.ExitCode() != 4 {
		t.Fatal(e)
	}
}
func TestLintAll(t *testing.T) {
	a := Parse("a.k7m2qx.a.md", []byte(strings.Replace(sample, "## Depends on\n", "## Depends on\n- p3rt9a\n- missing\n", 1)))
	b := Parse("b.p3rt9a.b.md", []byte(strings.Replace(strings.Replace(sample, "id: k7m2qx", "id: p3rt9a", 1), "## Depends on\n", "## Depends on\n- k7m2qx\n", 1)))
	dup := Parse("x.k7m2qx.dup.md", []byte(sample))
	findings := Lint([]*Document{a, b})
	findings = append(findings, Lint([]*Document{dup, dup})...)
	var probs []string
	for _, f := range findings {
		probs = append(probs, f.Problem)
		if f.Line < 1 || f.Path == "" {
			t.Fatal(f)
		}
	}
	for _, want := range []string{"dangling dependency missing", "dependency cycle involving k7m2qx", "duplicate id k7m2qx"} {
		if !contains(probs, want) {
			t.Fatal(want, probs)
		}
	}
	if reflect.DeepEqual(findings, []Finding{}) {
		t.Fatal("no findings")
	}
	t.Log(fmt.Sprint(findings))
}
