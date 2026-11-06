package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallThrowawayHomes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("JCODE_HOME", "")
	other := filepath.Join(home, "instance")
	os.MkdirAll(filepath.Join(other, ".jcode"), 0o700)
	os.MkdirAll(filepath.Join(home, ".jcode"), 0o700)
	os.WriteFile(filepath.Join(home, ".zshrc"), []byte("typeset -gA JCODE_INSTANCES=(\n demo '"+other+"'\n)\n"), 0o600)
	var out bytes.Buffer
	a := &App{Cwd: home, Out: &out, Err: &out}
	ctx := context.Background()
	for _, args := range [][]string{
		{"install", "jcode", "--all-instances", "--dry-run"},
		{"install", "jcode", "--all-instances"},
		{"install", "jcode", "--all-instances"},
		{"uninstall", "jcode", "--all-instances"},
		{"install", "jcode", "--jcode-home", filepath.Join(home, "explicit")},
		{"uninstall", "jcode", "--jcode-home", filepath.Join(home, "explicit"), "--json"},
	} {
		if code := a.Run(ctx, args); code != 0 {
			t.Fatal(args, code, out.String())
		}
	}
	for _, h := range []string{filepath.Join(home, ".jcode"), filepath.Join(other, ".jcode")} {
		b, e := os.ReadFile(filepath.Join(h, "mcp.json"))
		if e != nil || strings.Contains(string(b), `"huginn"`) {
			t.Fatal(h, string(b), e)
		}
	}
	if code := a.Run(ctx, []string{"install", "jcode", "--all-instances", "--jcode-home", home}); code != 2 {
		t.Fatal(code)
	}
	if code := a.Run(ctx, []string{"install", "other"}); code != 2 {
		t.Fatal(code)
	}
}

func TestOutsideGitFix(t *testing.T) {
	a := &App{Cwd: t.TempDir()}
	_, err := a.Execute(context.Background(), []string{"ls"})
	if err == nil {
		t.Fatal("expected error")
	}
	_, problem, fix := ErrorDetails(err)
	if !strings.Contains(problem, "Git checkout") || !strings.Contains(fix, "huginn init") {
		t.Fatal(problem, fix)
	}
}
