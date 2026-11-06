package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func installTest(home string, install, dry bool) (InstallResult, error) {
	return InstallJcodeHome(home, filepath.Join(os.TempDir(), "huginn"), install, dry)
}

func TestJcodeRealShapeAndLegacyMigration(t *testing.T) {
	fixture, err := os.ReadFile("testdata/jcode-mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"servers", "alias", "mixed", "empty"} {
		for _, install := range []bool{true, false} {
			t.Run(mode+map[bool]string{true: "-install", false: "-uninstall"}[install], func(t *testing.T) {
				home := t.TempDir()
				path := filepath.Join(home, "mcp.json")
				root := map[string]json.RawMessage{}
				json.Unmarshal(fixture, &root)
				original := root["servers"]
				key := "servers"
				switch mode {
				case "alias":
					root["mcpServers"] = root["servers"]
					delete(root, "servers")
					key = "mcpServers"
				case "mixed":
					root["mcpServers"] = json.RawMessage(`{"huginn":{"command":"huginn","args":["mcp"]},"other":{"command":"other"}}`)
				case "empty":
					root = map[string]json.RawMessage{}
				}
				if !install && mode != "mixed" {
					var m map[string]json.RawMessage
					json.Unmarshal(root[key], &m)
					if m == nil {
						m = map[string]json.RawMessage{}
					}
					m["huginn"] = json.RawMessage(`{"command":"huginn","args":["mcp"]}`)
					root[key], _ = json.Marshal(m)
				}
				before, _ := json.Marshal(root)
				os.WriteFile(path, before, 0o600)
				bin := filepath.Join(home, "bin", "huginn")
				r, err := InstallJcodeHome(home, bin, install, false)
				if err != nil || !r.Changed {
					t.Fatal(r, err)
				}
				b, _ := os.ReadFile(path)
				root = nil
				json.Unmarshal(b, &root)
				if len(root) != 1 || root[key] == nil {
					t.Fatal("duplicate schema or wrong key", string(b))
				}
				var servers map[string]json.RawMessage
				json.Unmarshal(root[key], &servers)
				if mode != "empty" {
					var old map[string]json.RawMessage
					json.Unmarshal(original, &old)
					if !equalJSON(old["muninn"], servers["muninn"]) {
						t.Fatal("changed muninn", string(b))
					}
				}
				if install {
					var entry struct {
						Command string            `json:"command"`
						Args    []string          `json:"args"`
						Env     map[string]string `json:"env"`
					}
					json.Unmarshal(servers["huginn"], &entry)
					if entry.Command != bin || !filepath.IsAbs(entry.Command) || len(entry.Args) != 1 || entry.Args[0] != "mcp" || entry.Env == nil || len(entry.Env) != 0 {
						t.Fatal(string(b))
					}
				} else if servers["huginn"] != nil {
					t.Fatal("uninstall left huginn", string(b))
				}
				r, err = InstallJcodeHome(home, bin, install, false)
				if err != nil || r.Changed {
					t.Fatal("not idempotent", r, err)
				}
			})
		}
	}
}

func TestReinstallUpdatesOwnedExecutable(t *testing.T) {
	home := t.TempDir()
	for _, version := range []string{"v0.1.0", "v0.1.1"} {
		bin := filepath.Join(home, version, "huginn")
		r, e := InstallJcodeHome(home, bin, true, false)
		if e != nil || !r.Changed {
			t.Fatal(r, e)
		}
	}
}

func TestMixedConflictsAreNotClobbered(t *testing.T) {
	root := map[string]json.RawMessage{"servers": json.RawMessage(`{"other":{"command":"a"}}`), "mcpServers": json.RawMessage(`{"other":{"command":"b"},"huginn":{"command":"huginn","args":["mcp"]}}`)}
	for _, install := range []bool{true, false} {
		if _, e := editServers(root, filepath.Join(os.TempDir(), "huginn"), install); e == nil {
			t.Fatal("conflict accepted")
		}
	}
}
