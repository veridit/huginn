package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	b, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %s %v", args, b, e)
	}
	return strings.TrimSpace(string(b))
}
func app(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-b", "main")
	git(t, dir, "config", "user.name", "Test")
	git(t, dir, "config", "user.email", "test@example.invalid")
	git(t, dir, "commit", "--allow-empty", "-m", "init")
	t.Setenv("HUGINN_USER", "tester")
	return &App{Cwd: dir, In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
}
func execute(t *testing.T, a *App, args ...string) Reply {
	t.Helper()
	r, e := a.Execute(context.Background(), args)
	if e != nil {
		t.Fatalf("%v: %v", args, e)
	}
	if r["next"] == "" {
		t.Fatalf("missing next: %v", r)
	}
	return r
}
func expectCode(t *testing.T, a *App, want int, args ...string) {
	t.Helper()
	_, e := a.Execute(context.Background(), args)
	if e == nil {
		t.Fatalf("%v succeeded", args)
	}
	code, p, fix := ErrorDetails(e)
	if code != want || p == "" || fix == "" || strings.Contains(p, "\n") {
		t.Fatalf("%v: got %d %q %q, want %d", args, code, p, fix, want)
	}
}
func TestLifecycleJSONContract(t *testing.T) {
	a := app(t)
	execute(t, a, "init")
	expectCode(t, a, 4, "next")
	execute(t, a, "rebalance")
	r := execute(t, a, "new", "API", "--goal", "API works", "--accept", "First", "--accept", "Second", "--type", "bug")
	id := r["task"].(Reply)["id"].(string)
	dep := execute(t, a, "new", "Form", "--goal", "Form works", "--accept", "UI", "--dep", id)
	form := dep["task"].(Reply)["id"].(string)
	expectCode(t, a, 3, "take", form)
	expectCode(t, a, 3, "done", id)
	execute(t, a, "take", id)
	execute(t, a, "take", id)
	execute(t, a, "next", "--mine")
	execute(t, a, "check", id, "1")
	execute(t, a, "check", id, "1")
	execute(t, a, "note", id, "Evidence here")
	execute(t, a, "check", id, "2")
	execute(t, a, "done", id)
	execute(t, a, "done", id)
	r = execute(t, a, "show", id)
	task := r["task"].(Reply)
	if task["notes"] != "Evidence here" {
		t.Fatal(task)
	}
	r = execute(t, a, "show", form)
	deps := r["task"].(Reply)["dependencies"].([]string)
	if len(deps) != 1 || deps[0] != id {
		t.Fatal(r)
	}
	execute(t, a, "set", form, "--title", "New Form", "--priority", "high", "--label", "ui")
	execute(t, a, "mv", form, "auth")
	execute(t, a, "move", form, "--top")
	execute(t, a, "rebalance")
	execute(t, a, "drop", form)
	execute(t, a, "drop", form)
	r = execute(t, a, "ls")
	if len(r["tasks"].([]Reply)) != 2 {
		t.Fatal(r)
	}
	r = execute(t, a, "lint")
	b, _ := json.Marshal(r["findings"])
	if string(b) != "[]" {
		t.Fatal(string(b))
	}
	git(t, filepath.Join(a.Cwd, ".huginn"), "diff", "--exit-code")
	if got := git(t, filepath.Join(a.Cwd, ".huginn"), "status", "--porcelain"); got != "" {
		t.Fatal(got)
	}
	os.Remove(filepath.Join(a.Cwd, ".git", "huginn", "cache.db"))
	r = execute(t, a, "ls")
	if len(r["tasks"].([]Reply)) != 2 {
		t.Fatal(r)
	}
}
func TestUsageAndText(t *testing.T) {
	a := app(t)
	for _, args := range [][]string{{"nex"}, {"next", "--typo", "bug"}, {"ls", "--status", "don"}, {"next", "--type", "bugs"}, {"new", "x"}, {"check", "abc", "zero"}} {
		expectCode(t, a, 2, args...)
	}
	if len(strings.Split(strings.TrimSpace(Help), "\n")) >= 40 {
		t.Fatal("help too long")
	}
	for _, args := range [][]string{nil, {"help"}, {"types"}, {"init"}, {"new", "A", "--goal", "G", "--accept", "C"}, {"ls"}, {"show", "a"}} {
		a.Out = &bytes.Buffer{}
		a.Err = &bytes.Buffer{}
		if code := a.Run(context.Background(), args); code != 0 {
			t.Fatalf("%v code %d: %s", args, code, a.Err)
		}
		lines := strings.Split(strings.TrimSpace(a.Out.(*bytes.Buffer).String()), "\n")
		if !strings.HasPrefix(lines[len(lines)-1], "next: huginn ") {
			t.Fatal(lines)
		}
	}
}
func TestJSONErrors(t *testing.T) {
	a := app(t)
	if code := a.Run(context.Background(), []string{"ls", "--status", "don", "--json"}); code != 2 {
		t.Fatal(code)
	}
	var r Reply
	if e := json.Unmarshal(a.Out.(*bytes.Buffer).Bytes(), &r); e != nil {
		t.Fatal(e)
	}
	if r["next"] == nil || r["fix"] == nil {
		t.Fatal(r)
	}
}
