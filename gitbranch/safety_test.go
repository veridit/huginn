package gitbranch

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSafetyAndCallbackFailure(t *testing.T) {
	root, _ := fixture(t, false)
	for _, dir := range []string{".", "..", filepath.Join("..", "outside"), ".git", filepath.Join(".git", "notes")} {
		_, err := Open(root, "notes", dir)
		var typed *Error
		if !errors.As(err, &typed) || typed.Fix == "" {
			t.Fatalf("unsafe directory %q: %v", dir, err)
		}
	}
	r := openRepo(t, root, "notes", ".notes")
	writeFile(t, r, "a.md", "a\n")
	must(t, r.Commit(nil, "initial"))
	before, err := r.Head()
	must(t, err)
	must(t, r.Sync())
	after, err := r.Head()
	must(t, err)
	if before != after {
		t.Fatal("no-op sync changed HEAD")
	}
	writeFile(t, r, "b.md", "b\n")
	called := false
	err = r.Update(func(*Repository) error { called = true; return nil })
	var typed *Error
	if !errors.As(err, &typed) || called {
		t.Fatalf("dirty update ran callback: %v", err)
	}
	must(t, r.Commit(nil, "save b"))
	for _, p := range []string{filepath.Join("..", "escape"), filepath.Join(root, "absolute")} {
		if err = r.Commit([]string{p}, "bad"); !errors.As(err, &typed) {
			t.Fatalf("unsafe commit path %q: %v", p, err)
		}
	}
	writeFile(t, r, "a.md", "staged\n")
	command(t, r.Dir, "add", "a.md")
	if err = r.Commit([]string{"b.md"}, "bad"); !errors.As(err, &typed) {
		t.Fatalf("pre-staged commit accepted: %v", err)
	}
	command(t, r.Dir, "restore", "--staged", ".")
	must(t, r.Commit(nil, "save staged"))
	sentinel := errors.New("callback validation failed")
	before, err = r.Head()
	must(t, err)
	err = r.Update(func(r *Repository) error { writeFile(t, r, "a.md", "callback edit\n"); return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	after, err = r.Head()
	must(t, err)
	if before != after {
		t.Fatal("failed callback committed")
	}
	body, err := os.ReadFile(filepath.Join(r.Dir, "a.md"))
	must(t, err)
	if string(body) != "callback edit\n" {
		t.Fatal("callback edit lost")
	}
	clean(t, root)
}

func TestPushFailureIsNotRace(t *testing.T) {
	a, _ := pair(t)
	remote := command(t, a.Root, "remote", "get-url", "origin")
	command(t, remote, "config", "receive.maxInputSize", "1")
	before, err := a.Head()
	must(t, err)
	err = a.Update(func(r *Repository) error {
		return os.WriteFile(filepath.Join(r.Dir, "task.md"), []byte("alice\n"), 0644)
	})
	var raced *ErrRaced
	var typed *Error
	if err == nil || errors.As(err, &raced) || !errors.As(err, &typed) {
		t.Fatalf("expected ordinary push error, got %v", err)
	}
	after, err := a.Head()
	must(t, err)
	if before == after {
		t.Fatal("push failure discarded local commit")
	}
	clean(t, a.Dir)
	clean(t, a.Root)
}

func TestFullSnapshotAndLiteralPaths(t *testing.T) {
	root, _ := fixture(t, false)
	r := openRepo(t, root, "notes", ".notes")
	writeFile(t, r, "one.md", "one\n")
	must(t, r.Commit(nil, "one"))
	writeFile(t, r, "two.md", "two\n")
	must(t, r.Commit(nil, "two"))
	h, err := r.Head()
	must(t, err)
	changes, err := r.DiffNameStatus("", h)
	must(t, err)
	if len(changes) != 2 {
		t.Fatalf("full snapshot omitted prior commit file: %#v", changes)
	}
	writeFile(t, r, "[literal].md", "literal\n")
	writeFile(t, r, "l.md", "unrelated\n")
	must(t, r.Commit([]string{"[literal].md"}, "literal"))
	newer, err := r.Head()
	must(t, err)
	changes, err = r.DiffNameStatus(h, newer)
	must(t, err)
	if len(changes) != 1 || changes[0].Path != "[literal].md" {
		t.Fatalf("path interpreted as wildcard: %#v", changes)
	}
	must(t, r.Commit(nil, "rest"))
	clean(t, r.Dir)
	clean(t, root)
}
