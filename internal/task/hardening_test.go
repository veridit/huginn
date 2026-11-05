package task

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestYAMLLosslessEdits(t *testing.T) {
	body := strings.Replace(sample, "title: Recall gate", "title: |\n  Recall\n  gate", 1)
	body = strings.Replace(body, "labels: [recall, bug]", "labels:\n  - recall\n  - bug", 1)
	opaque := "opaque:\n  nested:\n    - {something: 'untouched'} # keep me\n# unknown boundary comment\n"
	body = strings.Replace(body, "x-opaque:", opaque+"x-opaque:", 1)
	d := Parse("A.k7m2qx.recall.md", []byte(body))
	if len(d.Findings) > 0 || len(d.Task.Labels) != 2 || d.Task.Title != "Recall\ngate\n" {
		t.Fatal(d.Task, d.Findings)
	}
	if e := d.SetField("labels", []string{"new"}); e != nil {
		t.Fatal(e)
	}
	if e := d.SetField("title", "Updated"); e != nil {
		t.Fatal(e)
	}
	if !bytes.Contains(d.Bytes(), []byte(opaque)) || strings.Contains(string(d.Bytes()), "  - recall") || strings.Contains(string(d.Bytes()), "  Recall") {
		t.Fatal(string(d.Bytes()))
	}
	if len(d.Findings) > 0 {
		t.Fatal(d.Findings)
	}
}
func TestFencedSectionsNotSemantic(t *testing.T) {
	body := strings.Replace(sample, "## Notes\nExisting.", "## Notes\n```markdown\n## Depends on\n- missing\n## Acceptance\n- [ ] fake\n```\nExisting.", 1)
	d := Parse("A.k7m2qx.recall.md", []byte(body))
	if len(d.Task.Dependencies) != 0 || len(d.Task.Acceptance) != 2 || len(d.Findings) != 0 {
		t.Fatal(d.Task, d.Findings)
	}
}
func TestDepsConfig(t *testing.T) {
	s := &Store{Root: t.TempDir()}
	a := newTest(t, s, "A")
	b := newTest(t, s, "B", a.ID)
	if e := os.WriteFile(filepath.Join(s.Root, "huginn.toml"), []byte("deps = \"warn\" # documented\n"), 0644); e != nil {
		t.Fatal(e)
	}
	r, e := s.Take(b.ID, "alice")
	if e != nil || len(r.Warnings) != 1 {
		t.Fatal(r, e)
	}
	if e := os.WriteFile(filepath.Join(s.Root, "huginn.toml"), []byte("deps = \"blok\"\n"), 0644); e != nil {
		t.Fatal(e)
	}
	if _, e = DepsPolicy(s.Root); e == nil {
		t.Fatal("invalid policy accepted")
	}
}
