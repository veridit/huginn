package task

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func seedRank(t *testing.T, root, r, id string) {
	t.Helper()
	body := strings.Replace(sample, "k7m2qx", id, 1)
	path := BuildName(r, id, "task")
	if e := os.WriteFile(filepath.Join(root, path), []byte(body), 0644); e != nil {
		t.Fatal(e)
	}
}

func TestMoveCaseOnlyRankRename(t *testing.T) {
	s := &Store{Root: t.TempDir()}
	seedRank(t, s.Root, "A", "000001")
	seedRank(t, s.Root, "J", "000002")
	before := hashes(t, s.Root)
	r, err := s.Move("000001", MoveOptions{After: "000002"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Task.Rank != strings.ToLower(r.Task.Rank) {
		t.Fatalf("D19 generated uppercase rank %q", r.Task.Rank)
	}
	assertReportedWindow(t, s, before, r)
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if entry.Name() == r.Task.Path {
			found = true
		}
		if entry.Name() == BuildName("A", "000001", "task") {
			t.Fatal("old spelling remains after case-only rename")
		}
	}
	if !found {
		t.Fatal("destination spelling missing", r.Task.Path)
	}
}

func TestRenameDestinationRejectsOtherEntries(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	dest := filepath.Join(root, "other")
	if err := os.WriteFile(source, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(source, dest); err != nil {
		t.Fatal(err)
	}
	if available, err := renameDestinationAvailable(source, dest); err != nil || available {
		t.Fatalf("hard link must not be overwritten: available=%v err=%v", available, err)
	}
	caseDest := filepath.Join(root, "SOURCE")
	if _, err := os.Lstat(caseDest); os.IsNotExist(err) {
		if err := os.Link(source, caseDest); err != nil {
			t.Fatal(err)
		}
		if available, err := renameDestinationAvailable(source, caseDest); err != nil || available {
			t.Fatalf("case-equivalent distinct hard link must not be overwritten: available=%v err=%v", available, err)
		}
	}
}

func TestDirectCaseOnlyRename(t *testing.T) {
	s := &Store{Root: t.TempDir()}
	seedRank(t, s.Root, "A", "000001")
	d, err := s.Get("000001")
	if err != nil {
		t.Fatal(err)
	}
	before := hashes(t, s.Root)
	r, err := s.rename(d, "", "a", "task")
	if err != nil {
		t.Fatal(err)
	}
	assertReportedWindow(t, s, before, r)
	entries, err := os.ReadDir(s.Root)
	if err != nil || len(entries) != 1 || entries[0].Name() != r.Task.Path {
		t.Fatalf("case-only rename did not install exact destination spelling: %v %v", entries, err)
	}
}

func TestRandomMovesCaseInsensitiveSimulation(t *testing.T) {
	for _, seed := range []int64{7, 42, 2026} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			s := &Store{Root: t.TempDir()}
			for n, key := range []string{"0", "A", "A", "A0", "J", "a", "z", ""} {
				seedRank(t, s.Root, key, fmt.Sprintf("00000%d", n+1))
			}
			rng := rand.New(rand.NewSource(seed))
			for n := 0; n < 500; n++ {
				ts, err := s.Tasks()
				if err != nil {
					t.Fatal(err)
				}
				moved, neighbour := ts[rng.Intn(len(ts))], ts[rng.Intn(len(ts))]
				o := MoveOptions{Top: true}
				if n%3 == 1 {
					o = MoveOptions{Before: neighbour.ID}
				} else if n%3 == 2 {
					o = MoveOptions{After: neighbour.ID}
				}
				before := hashes(t, s.Root)
				r, err := s.Move(moved.ID, o)
				if err != nil {
					t.Fatalf("move %d: %v", n, err)
				}
				assertReportedWindow(t, s, before, r)
				ts, err = s.Tasks()
				if err != nil {
					t.Fatal(err)
				}
				folded := map[string]bool{}
				for _, task := range ts {
					key := strings.ToLower(task.Path)
					if folded[key] {
						t.Fatalf("move %d: case-fold collision %s", n, task.Path)
					}
					folded[key] = true
					if contains(r.Renamed, task.ID) && strings.Trim(task.Rank, "0123456789abcdefghijklmnopqrstuvwxyz") != "" {
						t.Fatalf("generated non-base36 rank %q", task.Rank)
					}
				}
			}
			t.Log("500 random top/before/after moves: lowercase-fold filenames unique, generated ranks lowercase, hashes unchanged outside reported windows")
		})
	}
}

func TestLintCaseInsensitiveFilenameCollision(t *testing.T) {
	a := Parse("A.000001.task.md", []byte(strings.Replace(sample, "k7m2qx", "000001", 1)))
	b := Parse("a.000001.task.md", a.Bytes())
	count := 0
	for _, finding := range Lint([]*Document{a, b}) {
		if strings.Contains(finding.Problem, "case-insensitive filename collision") {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("expected finding for each conflicting file, got %d", count)
	}
}
func assertReportedWindow(t *testing.T, s *Store, before map[string][32]byte, result Result) {
	t.Helper()
	after := hashes(t, s.Root)
	changed := map[string]bool{}
	for _, r := range result.Renames {
		changed[r.From] = true
		changed[r.To] = true
		if before[r.From] != after[r.To] {
			t.Fatal("renamed content changed", r)
		}
	}
	for path, want := range before {
		if !changed[path] && after[path] != want {
			t.Fatal("outside reported window changed", path)
		}
	}
	if len(result.Renamed) != len(result.Renames) {
		t.Fatal(result)
	}
	for _, rename := range result.Renames {
		if !contains(result.Renamed, rename.ID) {
			t.Fatal(result)
		}
	}
}
func TestAutomaticMinimalWindow(t *testing.T) {
	for _, tc := range []struct{ a, b string }{{"A", "A0"}, {"A", "A"}, {"000000000000", "0000000000000"}, {"!", "?"}} {
		t.Run(tc.a+"_"+tc.b, func(t *testing.T) {
			s := &Store{Root: t.TempDir()}
			seedRank(t, s.Root, "0", "000001")
			seedRank(t, s.Root, tc.a, "000002")
			seedRank(t, s.Root, tc.b, "000003")
			seedRank(t, s.Root, "z", "000004")
			seedRank(t, s.Root, "zz", "000005")
			seedRank(t, s.Root, "", "000006")
			before := hashes(t, s.Root)
			r, e := s.Move("000005", MoveOptions{Before: "000003"})
			if e != nil {
				t.Fatal(e)
			}
			if len(r.Renamed) > 2 {
				t.Fatal("repair not minimal", r.Renamed)
			}
			assertReportedWindow(t, s, before, r)
			ts, e := s.Tasks()
			if e != nil {
				t.Fatal(e)
			}
			var moved, right int
			for n, x := range ts {
				if x.ID == "000005" {
					moved = n
				}
				if x.ID == "000003" {
					right = n
				}
				if contains(r.Renamed, x.ID) && len(x.Rank) > 12 {
					t.Fatal("overlong", x)
				}
				if x.ID == "000006" && x.Rank != "" {
					t.Fatal("unranked changed")
				}
			}
			if moved+1 != right {
				t.Fatal("wrong order", ts)
			}
			t.Logf("repair %q/%q reported %v", tc.a, tc.b, r.Renamed)
		})
	}
}
func TestThousandRepeatedMoves(t *testing.T) {
	s := &Store{Root: t.TempDir()}
	for n, r := range []string{"A", "B", "C", "D", "E", ""} {
		seedRank(t, s.Root, r, fmt.Sprintf("00000%d", n+1))
	}
	for n := 0; n < 1000; n++ {
		before := hashes(t, s.Root)
		moved := "000004"
		if n%2 == 0 {
			moved = "000005"
		}
		o := MoveOptions{Top: true}
		if n%3 == 0 {
			o = MoveOptions{Before: "000003"}
		}
		r, e := s.Move(moved, o)
		if e != nil {
			t.Fatalf("move %d: %v", n, e)
		}
		assertReportedWindow(t, s, before, r)
		ts, e := s.Tasks()
		if e != nil {
			t.Fatal(e)
		}
		for _, x := range ts {
			if x.ID == "000006" && x.Rank != "" {
				t.Fatal("ranked unranked task")
			}
			if len(x.Rank) > 12 {
				t.Fatal("overlong rank")
			}
		}
		if o.Top && ts[0].ID != moved {
			t.Fatal(n, "not top", ts)
		}
		if o.Before != "" {
			for i, x := range ts {
				if x.ID == moved && (i+1 >= len(ts) || ts[i+1].ID != o.Before) {
					t.Fatal(n, "not before", ts)
				}
			}
		}
	}
	t.Log("1,000 alternating --top/--before moves: no errors, <=12-char keys, exact expected order, hashes unchanged outside reported window")
}
func TestAcceptanceBlockFixesAndMineDoing(t *testing.T) {
	s := &Store{Root: t.TempDir()}
	a := newTest(t, s, "A")
	b := newTest(t, s, "B")
	if _, e := s.Take(b.ID, "alice"); e != nil {
		t.Fatal(e)
	}
	r, e := s.Next(Filter{Mine: "alice"})
	if e != nil || r.Task.ID != b.ID {
		t.Fatal(r, e)
	}
	r, e = s.Next(Filter{})
	if e != nil || r.Task.ID != a.ID {
		t.Fatal(r, e)
	}
	d, e := s.Get(b.ID)
	if e != nil {
		t.Fatal(e)
	}
	before := d.Bytes()
	_, e = s.Done(b.ID)
	var te *Error
	if !errors.As(e, &te) || te.ExitCode() != 3 || len(te.Unchecked) != 1 || te.Unchecked[0].Fix != "huginn check "+b.ID+" 1" {
		t.Fatal(e)
	}
	d, e = s.Get(b.ID)
	if e != nil || !bytes.Equal(before, d.Bytes()) {
		t.Fatal("blocked done wrote file", e)
	}
	if e = os.WriteFile(filepath.Join(s.Root, "huginn.toml"), []byte("acceptance = \"warn\"\n"), 0644); e != nil {
		t.Fatal(e)
	}
	r, e = s.Done(b.ID)
	if e != nil || len(r.Warnings) != 1 {
		t.Fatal(r, e)
	}
	if _, e = s.Drop(a.ID); e != nil {
		t.Fatal(e)
	}
}
