package task

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDependenciesLosslessAndIdempotent(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		t.Run(strings.ReplaceAll(newline, "\n", "LF"), func(t *testing.T) {
			s := &Store{Root: t.TempDir()}
			seedRank(t, s.Root, "a", "000001")
			seedRank(t, s.Root, "b", "000002")
			seedRank(t, s.Root, "c", "000003")
			for _, id := range []string{"000002", "000003"} {
				d, err := s.Get(id)
				if err != nil {
					t.Fatal(err)
				}
				if err := d.SetField("aliases", []string{"old-" + id}); err != nil {
					t.Fatal(err)
				}
				if err := AtomicWrite(filepath.Join(s.Root, d.Task.Path), d.Bytes()); err != nil {
					t.Fatal(err)
				}
			}
			d, err := s.Get("000001")
			if err != nil {
				t.Fatal(err)
			}
			raw := strings.Replace(string(d.Bytes()), "## Depends on\n\n", "## Depends on\n- OLD-000002 keep  two spaces # note\n\n", 1)
			raw = strings.ReplaceAll(raw, "\n", newline)
			path := filepath.Join(s.Root, d.Task.Path)
			if err := AtomicWrite(path, []byte(raw)); err != nil {
				t.Fatal(err)
			}
			others := hashes(t, s.Root)
			r, err := s.AddDependencies("000001", []string{"OLD-000002", "old-000003", "000003"})
			if err != nil || !r.Changed || r.Next != "huginn take 000001" {
				t.Fatal(r, err)
			}
			want := strings.Replace(raw, "## Acceptance", "- 000003"+newline+"## Acceptance", 1)
			got, err := os.ReadFile(path)
			if err != nil || string(got) != want {
				t.Fatalf("add changed unrelated bytes: got %q want %q err %v", got, want, err)
			}
			r, err = s.AddDependencies("000001", []string{"000002", "old-000003"})
			if err != nil || r.Changed {
				t.Fatal("repeat add must be noop", r, err)
			}
			r, err = s.RemoveDependencies("000001", []string{"OLD-000003"})
			if err != nil || !r.Changed {
				t.Fatal(r, err)
			}
			got, err = os.ReadFile(path)
			if err != nil || string(got) != raw {
				t.Fatalf("add/remove roundtrip changed original bytes: %q %v", got, err)
			}
			r, err = s.RemoveDependencies("000001", []string{"000003"})
			if err != nil || r.Changed {
				t.Fatal("repeat remove must be noop", r, err)
			}
			r, err = s.RemoveDependencies("000001", []string{"000002"})
			if err != nil || !r.Changed {
				t.Fatal(r, err)
			}
			want = strings.Replace(raw, "- OLD-000002 keep  two spaces # note"+newline, "", 1)
			got, err = os.ReadFile(path)
			if err != nil || string(got) != want {
				t.Fatalf("remove changed unrelated bytes: %q %v", got, err)
			}
			for path, hash := range hashes(t, s.Root) {
				if path != d.Task.Path && others[path] != hash {
					t.Fatal("other file changed", path)
				}
			}
		})
	}
}

func TestDependenciesCreateSectionAndDanglingRepair(t *testing.T) {
	for _, ending := range []string{"", "\n", "\r\n"} {
		s := &Store{Root: t.TempDir()}
		seedRank(t, s.Root, "a", "000001")
		seedRank(t, s.Root, "b", "000002")
		d, _ := s.Get("000001")
		raw := strings.TrimSuffix(strings.Replace(string(d.Bytes()), "## Depends on\n\n", "", 1), "\n") + ending
		path := filepath.Join(s.Root, d.Task.Path)
		if err := AtomicWrite(path, []byte(raw)); err != nil {
			t.Fatal(err)
		}
		r, err := s.AddDependencies("000001", []string{"000002"})
		if err != nil || !r.Changed || len(r.Task.Dependencies) != 1 || r.Task.Dependencies[0].Ref != "000002" {
			t.Fatal(r, err)
		}
		after, _ := os.ReadFile(path)
		if !bytes.HasPrefix(after, []byte(raw)) {
			t.Fatal("creating section changed existing bytes")
		}
		target, _ := s.Get("000002")
		if err := os.Remove(filepath.Join(s.Root, target.Task.Path)); err != nil {
			t.Fatal(err)
		}
		r, err = s.RemoveDependencies("000001", []string{"000002"})
		if err != nil || !r.Changed || len(r.Task.Dependencies) != 0 {
			t.Fatal("cannot repair dangling dependency", r, err)
		}
		r, err = s.RemoveDependencies("000001", []string{"000002"})
		if err != nil || r.Changed {
			t.Fatal("removing absent deleted target must be noop", r, err)
		}
	}
}

func TestDependencyCycleRejectsBatchAtomically(t *testing.T) {
	s := &Store{Root: t.TempDir()}
	a := newTest(t, s, "Alpha")
	b := newTest(t, s, "Beta", a.ID)
	c := newTest(t, s, "Gamma", b.ID)
	d := newTest(t, s, "Delta")
	before := hashes(t, s.Root)
	_, err := s.AddDependencies(a.ID, []string{d.ID, c.ID})
	var typed *Error
	want := []string{a.ID, c.ID, b.ID, a.ID}
	if !errors.As(err, &typed) || typed.Kind != "blocked" || typed.ExitCode() != 3 || !reflect.DeepEqual(typed.Cycle, want) || typed.Fix != "huginn dep "+c.ID+" --remove "+b.ID {
		t.Fatalf("missing canonical cycle/fix: %#v %v", typed, err)
	}
	if !strings.Contains(err.Error(), strings.Join(want, " -> ")) {
		t.Fatal("cycle path not in text error", err)
	}
	if len(typed.Dependencies) != 3 || !strings.Contains(err.Error(), "Alpha") || !strings.Contains(err.Error(), "Beta") || !strings.Contains(err.Error(), "Gamma") {
		t.Fatal("cycle error omitted task titles", err)
	}
	if !reflect.DeepEqual(before, hashes(t, s.Root)) {
		t.Fatal("rejected batch wrote files")
	}
	_, err = s.AddDependencies(a.ID, []string{a.ID})
	if !errors.As(err, &typed) || !reflect.DeepEqual(typed.Cycle, []string{a.ID, a.ID}) {
		t.Fatal("self-cycle accepted", err)
	}
	_, err = s.AddDependencies(a.ID, []string{d.ID, "does-not-exist"})
	if err == nil || !reflect.DeepEqual(before, hashes(t, s.Root)) {
		t.Fatal("partially applied invalid batch", err)
	}
}

func TestDependenciesDoNotWriteHiddenFence(t *testing.T) {
	s := &Store{Root: t.TempDir()}
	a := newTest(t, s, "Alpha")
	b := newTest(t, s, "Beta")
	d, _ := s.Get(a.ID)
	path := filepath.Join(s.Root, d.Task.Path)
	broken := strings.Replace(string(d.Bytes()), "## Depends on\n", "## Depends on\n```\n", 1)
	if err := AtomicWrite(path, []byte(broken)); err != nil {
		t.Fatal(err)
	}
	_, err := s.AddDependencies(a.ID, []string{b.ID})
	var typed *Error
	if !errors.As(err, &typed) || typed.Kind != "broken-file" {
		t.Fatal("hidden addition not rejected", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != broken {
		t.Fatal("broken file changed")
	}
}

func TestRemoveDependenciesKeepsFencedNotes(t *testing.T) {
	s := &Store{Root: t.TempDir()}
	seedRank(t, s.Root, "a", "000001")
	seedRank(t, s.Root, "b", "000002")
	d, _ := s.Get("000001")
	raw := strings.Replace(string(d.Bytes()), "## Depends on\n\n", "## Depends on\n- 000002 first note\n```markdown\n- 000002 code is not an edge\n```\n000002 second note\n\n", 1)
	path := filepath.Join(s.Root, d.Task.Path)
	if err := AtomicWrite(path, []byte(raw)); err != nil {
		t.Fatal(err)
	}
	r, err := s.RemoveDependencies("000001", []string{"000002", "000002"})
	if err != nil || !r.Changed || len(r.Task.Dependencies) != 0 {
		t.Fatal(r, err)
	}
	want := strings.Replace(strings.Replace(raw, "- 000002 first note\n", "", 1), "000002 second note\n", "", 1)
	got, _ := os.ReadFile(path)
	if string(got) != want {
		t.Fatalf("fenced notes or unrelated bytes changed: %q", got)
	}
	before, _ := os.Stat(path)
	r, err = s.RemoveDependencies("000001", []string{"000002"})
	after, _ := os.Stat(path)
	if err != nil || r.Changed || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("noop remove rewrote file", r, err)
	}
}
