package task

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// DepsPolicy reads the project's dependency policy, defaulting to block.
// Unrelated TOML keys are left to their owning components.
func DepsPolicy(root string) (string, error) {
	return readPolicy(root, "deps")
}

// AcceptancePolicy defaults to block, mirroring DepsPolicy.
func AcceptancePolicy(root string) (string, error) { return readPolicy(root, "acceptance") }
func readPolicy(root, field string) (string, error) {
	f, e := os.Open(filepath.Join(root, "huginn.toml"))
	if os.IsNotExist(e) {
		return "block", nil
	}
	if e != nil {
		return "", e
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	section := ""
	policy := "block"
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if strings.HasPrefix(line, "[") {
			section = line
			continue
		}
		if section != "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != field {
			continue
		}
		value = scalar(value)
		if !contains([]string{"block", "warn"}, value) {
			e := Invalid(field, value, []string{"block", "warn"})
			e.Fix = "set " + field + " = \"block\" in huginn.toml"
			return "", e
		}
		policy = value
	}
	return policy, scan.Err()
}
