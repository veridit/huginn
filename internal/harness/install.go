package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// InstallResult describes an actual or dry-run MCP configuration change.
type InstallResult struct {
	File    string `json:"file"`
	Backup  string `json:"backup,omitempty"`
	Changed bool   `json:"changed"`
}

// InstallJcodeHome changes only the huginn server. Backups are private, uniquely
// named, and written before atomic replacement of an existing configuration.
func InstallJcodeHome(home, bin string, install, dry bool) (InstallResult, error) {
	path := filepath.Join(home, "mcp.json")
	res := InstallResult{File: path}
	if fi, err := os.Lstat(path); err == nil && !fi.Mode().IsRegular() {
		return res, fmt.Errorf("%s must be a regular file, not a symlink or directory", path)
	}
	root := map[string]json.RawMessage{}
	before, err := os.ReadFile(path)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return res, err
	}
	if exists {
		if len(bytes.TrimSpace(before)) == 0 {
			return res, fmt.Errorf("%s is empty, expected a JSON object", path)
		}
		if err := json.Unmarshal(before, &root); err != nil {
			return res, fmt.Errorf("%s: %w", path, err)
		}
		if root == nil {
			return res, fmt.Errorf("%s must be a JSON object", path)
		}
	}
	changed, err := editServers(root, bin, install)
	if err != nil {
		return res, fmt.Errorf("%s: %w", path, err)
	}
	if !changed {
		return res, nil
	}
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return res, err
	}
	out = append(out, '\n')
	res.Changed = true
	if dry {
		return res, nil
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return res, err
	}
	if exists {
		f, err := os.CreateTemp(home, "mcp.json.huginn-bak-"+time.Now().UTC().Format("20060102T150405")+"-*")
		if err != nil {
			return res, err
		}
		res.Backup = f.Name()
		_, err = f.Write(before)
		closeErr := f.Close()
		if err != nil {
			return res, err
		}
		if closeErr != nil {
			return res, closeErr
		}
	}
	f, err := os.CreateTemp(home, ".huginn-mcp-*")
	if err != nil {
		return res, err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(out); err != nil {
		f.Close()
		return res, err
	}
	if err = f.Close(); err != nil {
		return res, err
	}
	return res, os.Rename(f.Name(), path)
}
