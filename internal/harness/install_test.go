package harness

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallLifecycle(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "mcp.json")
	original := []byte(`{"custom":{"keep":true},"mcpServers":{"other":{"command":"other","args":["serve"]}}}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := InstallJcodeHome(home, true, true)
	if err != nil || !r.Changed {
		t.Fatal(r, err)
	}
	b, _ := os.ReadFile(path)
	if !bytes.Equal(b, original) {
		t.Fatal("dry run wrote config")
	}
	entries, _ := os.ReadDir(home)
	if len(entries) != 1 {
		t.Fatal("dry run wrote files")
	}
	r, err = InstallJcodeHome(home, true, false)
	if err != nil || !r.Changed || r.Backup == "" {
		t.Fatal(r, err)
	}
	b, _ = os.ReadFile(r.Backup)
	if !bytes.Equal(b, original) {
		t.Fatal("backup differs")
	}
	installed, _ := os.ReadFile(path)
	var root map[string]json.RawMessage
	if err := json.Unmarshal(installed, &root); err != nil {
		t.Fatal(err)
	}
	if string(root["custom"]) == "" {
		t.Fatal("lost custom field")
	}
	var servers map[string]json.RawMessage
	json.Unmarshal(root["mcpServers"], &servers)
	if len(servers) != 2 || !bytes.Contains(servers["huginn"], []byte(`"mcp"`)) {
		t.Fatal(string(installed))
	}
	r, err = InstallJcodeHome(home, true, false)
	if err != nil || r.Changed {
		t.Fatal(r, err)
	}
	again, _ := os.ReadFile(path)
	if !bytes.Equal(again, installed) {
		t.Fatal("non-idempotent install")
	}
	r, err = InstallJcodeHome(home, false, true)
	if err != nil || !r.Changed {
		t.Fatal(r, err)
	}
	again, _ = os.ReadFile(path)
	if !bytes.Equal(again, installed) {
		t.Fatal("dry uninstall wrote")
	}
	r, err = InstallJcodeHome(home, false, false)
	if err != nil || !r.Changed {
		t.Fatal(r, err)
	}
	b, _ = os.ReadFile(path)
	json.Unmarshal(b, &root)
	json.Unmarshal(root["mcpServers"], &servers)
	// Unmarshal into a fresh map because encoding/json merges maps.
	servers = nil
	json.Unmarshal(root["mcpServers"], &servers)
	if len(servers) != 1 || servers["other"] == nil {
		t.Fatal(string(b))
	}
	r, err = InstallJcodeHome(home, false, false)
	if err != nil || r.Changed {
		t.Fatal(r, err)
	}
}

func TestMalformedAndConflictingConfigs(t *testing.T) {
	for _, input := range []string{"", "null", "[]", "{", `{"mcpServers":null}`, `{"mcpServers":[]}`, `{"mcpServers":{"huginn":{"command":"unrelated"}}}`} {
		t.Run(input, func(t *testing.T) {
			home := t.TempDir()
			path := filepath.Join(home, "mcp.json")
			os.WriteFile(path, []byte(input), 0o600)
			if _, err := InstallJcodeHome(home, true, false); err == nil {
				t.Fatal("accepted unsafe config")
			}
			b, _ := os.ReadFile(path)
			if string(b) != input {
				t.Fatal("changed invalid config")
			}
		})
	}
}

func TestMissingAndDiscovery(t *testing.T) {
	home := t.TempDir()
	if got := DiscoverJcode(home, nil); len(got) != 0 {
		t.Fatal(got)
	}
	target := filepath.Join(home, ".jcode")
	if r, e := InstallJcodeHome(target, false, false); e != nil || r.Changed {
		t.Fatal(r, e)
	}
	if _, e := os.Stat(target); !os.IsNotExist(e) {
		t.Fatal("uninstall created directory")
	}
	if r, e := InstallJcodeHome(target, true, true); e != nil || !r.Changed {
		t.Fatal(r, e)
	}
	if _, e := os.Stat(target); !os.IsNotExist(e) {
		t.Fatal("dry install created directory")
	}
	if _, e := InstallJcodeHome(target, true, false); e != nil {
		t.Fatal(e)
	}
	project := filepath.Join(home, "project space")
	os.MkdirAll(filepath.Join(project, ".jcode"), 0o700)
	rc := `typeset -gA JCODE_INSTANCES=(
 demo "$HOME/project space"
 duplicate "$HOME/project space"
)`
	os.WriteFile(filepath.Join(home, ".zshrc"), []byte(rc), 0o600)
	got := DiscoverJcode(home, []string{target})
	if len(got) != 2 || got[1].Home != filepath.Join(project, ".jcode") {
		t.Fatal(got)
	}
}
