package harness

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
)

func equalJSON(a, b json.RawMessage) bool {
	var x, y any
	return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && reflect.DeepEqual(x, y)
}

// Jcode's McpConfig has servers with a serde alias mcpServers. Either alone is
// valid, but both cause a duplicate-field error. Keep the existing shape when
// possible, preferring servers, and consolidate old mixed-shape installations.
func editServers(root map[string]json.RawMessage, bin string, install bool) (bool, error) {
	key := "servers"
	if _, ok := root[key]; !ok {
		if _, ok := root["mcpServers"]; ok {
			key = "mcpServers"
		}
	}
	maps := map[string]map[string]json.RawMessage{}
	for _, k := range []string{"servers", "mcpServers"} {
		if raw, ok := root[k]; ok {
			var entries map[string]json.RawMessage
			if err := json.Unmarshal(raw, &entries); err != nil || entries == nil {
				return false, fmt.Errorf("%s must be an object", k)
			}
			maps[k] = entries
		}
	}
	var want json.RawMessage
	if install {
		if !filepath.IsAbs(bin) {
			return false, fmt.Errorf("huginn command must be an absolute executable path")
		}
		want, _ = json.Marshal(map[string]any{"command": bin, "args": []string{"mcp"}, "env": map[string]string{}})
	}
	servers := map[string]json.RawMessage{}
	found := false
	for _, k := range []string{"servers", "mcpServers"} {
		for name, raw := range maps[k] {
			if name == "huginn" {
				found = true
				if install && !equalJSON(raw, want) && !ownedHuginn(raw) {
					return false, fmt.Errorf("huginn server already has a different configuration; rename it or uninstall it explicitly first")
				}
				continue
			}
			if old, ok := servers[name]; ok && !equalJSON(old, raw) {
				return false, fmt.Errorf("conflicting server %s in servers and mcpServers; reconcile it before retrying", name)
			}
			servers[name] = raw
		}
	}
	if !install && !found {
		return false, nil
	}
	if install {
		servers["huginn"] = want
	}
	raw, err := json.Marshal(servers)
	if err != nil {
		return false, err
	}
	// Compare the whole map, not just huginn, so mixed-schema migration is saved.
	changed := len(maps) > 1 || !equalJSON(root[key], raw)
	if !changed {
		return false, nil
	}
	delete(root, "servers")
	delete(root, "mcpServers")
	root[key] = raw
	return true, nil
}

func ownedHuginn(raw json.RawMessage) bool {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return false
	}
	for k := range m {
		if k != "command" && k != "args" && k != "env" {
			return false
		}
	}
	var command string
	var args []string
	if json.Unmarshal(m["command"], &command) != nil || json.Unmarshal(m["args"], &args) != nil {
		return false
	}
	if filepath.Base(command) != "huginn" && filepath.Base(command) != "huginn.exe" {
		return false
	}
	if !reflect.DeepEqual(args, []string{"mcp"}) {
		return false
	}
	if raw, ok := m["env"]; ok {
		var env map[string]string
		if json.Unmarshal(raw, &env) != nil || len(env) != 0 {
			return false
		}
	}
	return true
}
