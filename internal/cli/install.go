package cli

import (
	"os"
	"path/filepath"

	"github.com/veridit/huginn/internal/harness"
)

func (a *App) runInstall(cmd string, f flags) (Reply, error) {
	if err := f.count(1); err != nil {
		return nil, err
	}
	if f.args[0] != "jcode" {
		return nil, usage("supported harness: jcode", "huginn "+cmd+" jcode")
	}
	if f.has("all-instances") && f.has("jcode-home") {
		return nil, usage("--all-instances and --jcode-home are mutually exclusive", "huginn "+cmd+" jcode --help")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	var homes []string
	switch {
	case f.has("all-instances"):
		extras := []string{}
		if h := os.Getenv("JCODE_HOME"); h != "" {
			extras = append(extras, h)
		}
		for _, i := range harness.DiscoverJcode(home, extras) {
			homes = append(homes, i.Home)
		}
		if len(homes) == 0 {
			return nil, usage("no Jcode instances found", "huginn "+cmd+" jcode --jcode-home PATH")
		}
	case f.has("jcode-home"):
		if f.get("jcode-home") == "" {
			return nil, usage("--jcode-home cannot be empty", "huginn "+cmd+" jcode --help")
		}
		h := f.get("jcode-home")
		if !filepath.IsAbs(h) {
			h = filepath.Join(a.Cwd, h)
		}
		homes = []string{h}
	default:
		h := os.Getenv("JCODE_HOME")
		if h == "" {
			h = filepath.Join(home, ".jcode")
		}
		homes = []string{h}
	}
	bin, err := os.Executable()
	if err != nil {
		return nil, err
	}
	// Preflight every instance before modifying any of them.
	for _, h := range homes {
		if _, err := harness.InstallJcodeHome(h, bin, cmd == "install", true); err != nil {
			return nil, err
		}
	}
	results := []harness.InstallResult{}
	for _, h := range homes {
		r, err := harness.InstallJcodeHome(h, bin, cmd == "install", f.has("dry-run"))
		if err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return Reply{"installations": results, "dry_run": f.has("dry-run"), "next": "huginn next"}, nil
}
