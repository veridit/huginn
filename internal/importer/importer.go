// Package importer migrates backlog-md files without changing their source.
package importer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/veridit/huginn/internal/id"
	"github.com/veridit/huginn/internal/task"
	"gopkg.in/yaml.v3"
)

type Options struct {
	DryRun bool `json:"dry_run"`
}
type Report struct {
	Added     int          `json:"added"`
	Updated   int          `json:"updated"`
	Unchanged int          `json:"unchanged"`
	Skipped   int          `json:"skipped"`
	Files     []FileReport `json:"files"`
}
type FileReport struct {
	Source   string         `json:"source"`
	ID       string         `json:"id,omitempty"`
	Path     string         `json:"path,omitempty"`
	Action   string         `json:"action"`
	Findings []task.Finding `json:"findings"`
}
type source struct {
	path, alias, title string
	raw                []byte
	fields             map[string]any
	ordinal            float64
	ranked             bool
	report             int
}

// front separates valid line-oriented YAML from Markdown. YAML nodes supply
// field boundaries, so block scalars and nested unknown keys stay untouched.
func front(raw []byte) (map[string]any, int, error) {
	lines := strings.SplitAfter(string(raw), "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "---" {
		return nil, 0, fmt.Errorf("missing frontmatter")
	}
	offset := len(lines[0])
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			var node yaml.Node
			if err := yaml.Unmarshal(raw[len(lines[0]):offset], &node); err != nil {
				return nil, 0, err
			}
			if len(node.Content) != 1 || node.Content[0].Kind != yaml.MappingNode || node.Content[0].Style&yaml.FlowStyle != 0 {
				return nil, 0, fmt.Errorf("frontmatter must be a line-oriented mapping")
			}
			fields := map[string]any{}
			if err := node.Content[0].Decode(&fields); err != nil {
				return nil, 0, err
			}
			return fields, offset + len(lines[i]), nil
		}
		offset += len(lines[i])
	}
	return nil, 0, fmt.Errorf("unterminated frontmatter")
}
func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func list(v any) []string {
	var out []string
	switch x := v.(type) {
	case []any:
		for _, v := range x {
			out = append(out, str(v))
		}
	case []string:
		out = append(out, x...)
	case string:
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}
func canonical(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }
func stableID(alias string, attempt int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("backlog:%s:%d", canonical(alias), attempt)))
	n := binary.BigEndian.Uint32(sum[:4]) & 0x3fffffff
	const alphabet = "0123456789abcdefghjkmnpqrstvwxyz"
	b := make([]byte, 6)
	for i := 5; i >= 0; i-- {
		b[i] = alphabet[n&31]
		n >>= 5
	}
	return string(b)
}

// Run imports regular Markdown files from src. Existing aliases determine identity.
// Source changes merge by field and top-level section against the previous import.
// A locally edited unit wins, including when upstream also edited it. Existing
// paths are never renamed. Import baselines are written beside tasks, not in cache.
func Run(ctx context.Context, src string, dst *task.Store, opts Options) (Report, error) {
	report := Report{Files: []FileReport{}}
	if dst == nil || dst.Root == "" {
		return report, fmt.Errorf("destination store root is required")
	}
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return report, err
	}
	dstAbs, err := filepath.Abs(dst.Root)
	if err != nil {
		return report, err
	}
	storeCopy := *dst
	storeCopy.Root = dstAbs
	dst = &storeCopy
	if resolved, e := filepath.EvalSymlinks(srcAbs); e == nil {
		srcAbs = resolved
	}
	if resolved, e := filepath.EvalSymlinks(dstAbs); e == nil {
		dstAbs = resolved
		dst.Root = resolved
	}
	if srcAbs == dstAbs || strings.HasPrefix(dstAbs, srcAbs+string(os.PathSeparator)) || strings.HasPrefix(srcAbs, dstAbs+string(os.PathSeparator)) {
		return report, fmt.Errorf("source and destination must not overlap")
	}
	if err := checkDestination(dst.Root); err != nil {
		return report, err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return report, err
	}
	var sources []source
	seen := map[string]bool{}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			continue
		}
		path := filepath.Join(src, entry.Name())
		fr := FileReport{Source: path, Action: "skipped", Findings: []task.Finding{}}
		report.Files = append(report.Files, fr)
		idx := len(report.Files) - 1
		skip := func(problem string) {
			report.Skipped++
			report.Files[idx].Findings = append(report.Files[idx].Findings, task.Finding{Path: path, Line: 1, Problem: problem})
		}
		if !entry.Type().IsRegular() {
			skip("source is not a regular file")
			continue
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			return report, e
		}
		fields, _, e := front(raw)
		if e != nil {
			skip(e.Error())
			continue
		}
		alias := canonical(str(fields["id"]))
		if alias == "" {
			skip("missing source id")
			continue
		}
		if seen[alias] {
			return report, fmt.Errorf("duplicate source id %s", alias)
		}
		seen[alias] = true
		s := source{path: path, alias: alias, title: str(fields["title"]), raw: raw, fields: fields, report: idx}
		if value, ok := fields["ordinal"]; ok {
			s.ordinal, e = strconv.ParseFloat(str(value), 64)
			s.ranked = e == nil && !math.IsNaN(s.ordinal) && !math.IsInf(s.ordinal, 0)
			if !s.ranked { s.ordinal = 0 }
		}
		sources = append(sources, s)
	}
	sort.Slice(sources, func(i, j int) bool {
		a, b := sources[i], sources[j]
		if a.ranked != b.ranked {
			return a.ranked
		}
		if a.ordinal != b.ordinal {
			return a.ordinal < b.ordinal
		}
		return a.alias < b.alias
	})
	existing, err := dst.Tasks()
	if err != nil && !os.IsNotExist(err) {
		return report, err
	}
	ids := map[string]string{}
	occupied := map[string]bool{}
	byID := map[string]task.Task{}
	for _, t := range existing {
		if !id.Valid(t.ID) {
			return report, fmt.Errorf("invalid destination id %q: run huginn lint", t.ID)
		}
		if occupied[t.ID] {
			return report, fmt.Errorf("duplicate destination id %s", t.ID)
		}
		occupied[t.ID] = true
		byID[t.ID] = t
		for _, a := range t.Aliases {
			a = canonical(a)
			if old := ids[a]; old != "" && old != t.ID {
				return report, fmt.Errorf("ambiguous destination alias %s", a)
			}
			ids[a] = t.ID
		}
	}
	// Resolve through persisted baselines too: deleting an alias locally must not
	// cause a second task to appear on the next import.
	states, err := os.ReadDir(filepath.Join(dst.Root, ".backlog-import"))
	if err != nil && !os.IsNotExist(err) {
		return report, err
	}
	for _, entry := range states {
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		b, e := os.ReadFile(filepath.Join(dst.Root, ".backlog-import", entry.Name()))
		if e != nil {
			return report, e
		}
		var raw []byte
		if e = json.Unmarshal(b, &raw); e != nil {
			return report, fmt.Errorf("invalid import baseline %s: %w", entry.Name(), e)
		}
		t := task.Parse("", raw).Task
		if _, exists := byID[t.ID]; !exists {
			continue
		}
		for _, alias := range t.Aliases {
			a := canonical(alias)
			if old := ids[a]; old != "" && old != t.ID {
				return report, fmt.Errorf("ambiguous import baseline alias %s", a)
			}
			ids[a] = t.ID
		}
	}
	for _, s := range sources {
		if ids[s.alias] != "" {
			continue
		}
		for attempt := 0; ; attempt++ {
			id := stableID(s.alias, attempt)
			if !occupied[id] {
				ids[s.alias] = id
				occupied[id] = true
				break
			}
		}
	}
	for i, s := range sources {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		id := ids[s.alias]
		fr := &report.Files[s.report]
		fr.ID = id
		// Fixed-width ordinal position keys preserve numeric order and source-id ties.
		r := ""
		if s.ranked {
			r = fmt.Sprintf("%012d", i+1)
		}
		path := filepath.Join(dst.Root, task.BuildName(r, id, task.Slug(s.title)))
		old, exists := byID[id]
		if !exists {
			if _, e := os.Lstat(path); e == nil {
				return report, fmt.Errorf("destination filename already exists: %s", path)
			} else if !os.IsNotExist(e) {
				return report, e
			}
		}
		if exists {
			path = old.Path
			if !filepath.IsAbs(path) {
				path = filepath.Join(dst.Root, path)
			}
		}
		if rel, e := filepath.Rel(dstAbs, path); e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return report, fmt.Errorf("destination task path escapes root: %s", path)
		}
		fr.Path = path
		generated, e := convert(s, id, ids)
		if e != nil {
			return report, e
		}
		baselinePath := filepath.Join(dst.Root, ".backlog-import", id+".json")
		result := generated
		if exists {
			local, e := os.ReadFile(path)
			if e != nil {
				return report, e
			}
			baseline, e := os.ReadFile(baselinePath)
			if e != nil && !os.IsNotExist(e) {
				return report, e
			}
			if os.IsNotExist(e) {
				result = local
				fr.Findings = append(fr.Findings, task.Finding{Path: path, Line: 1, Problem: "existing alias has no import baseline: kept destination unchanged"})
			} else {
				var base []byte
				if e = json.Unmarshal(baseline, &base); e != nil {
					return report, fmt.Errorf("import baseline %s: %w", baselinePath, e)
				}
				result, e = merge(base, local, generated)
				if e != nil {
					return report, fmt.Errorf("merge %s: %w", path, e)
				}
			}
			if bytes.Equal(local, result) {
				report.Unchanged++
				fr.Action = "unchanged"
			} else {
				report.Updated++
				fr.Action = "updated"
			}
		} else {
			report.Added++
			fr.Action = "added"
		}
		fr.Findings = append(fr.Findings, task.Parse(path, result).Findings...)
		for _, dep := range list(s.fields["dependencies"]) {
			if ids[canonical(dep)] == "" {
				fr.Findings = append(fr.Findings, task.Finding{Path: path, Line: 1, Problem: "unresolved source dependency: " + dep})
			}
		}
		if !opts.DryRun {
			if fr.Action != "unchanged" {
				if e = task.AtomicWrite(path, result); e != nil {
					return report, e
				}
			}
			state, _ := json.Marshal(generated)
			previous, e := os.ReadFile(baselinePath)
			if e != nil && !os.IsNotExist(e) {
				return report, e
			}
			if !bytes.Equal(previous, state) {
				if e = task.AtomicWrite(baselinePath, state); e != nil {
					return report, e
				}
			}
		}
	}
	return report, nil
}

func checkDestination(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if os.IsNotExist(err) && path == root {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("destination contains symlink: %s", path)
		}
		if entry.IsDir() && strings.HasPrefix(entry.Name(), ".") && entry.Name() != ".backlog-import" && path != root {
			return filepath.SkipDir
		}
		return nil
	})
}

func convert(s source, id string, ids map[string]string) ([]byte, error) {
	_, end, err := front(s.raw)
	if err != nil {
		return nil, err
	}
	body := string(s.raw[end:])
	// Only headings outside fenced code blocks are semantic.
	lines := strings.SplitAfter(body, "\n")
	fence := ""
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			if fence == "" {
				fence = trim[:3]
			} else if strings.HasPrefix(trim, fence) {
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		switch strings.TrimSpace(line) {
		case "## Description":
			lines[i] = strings.Replace(line, "Description", "Goal", 1)
		case "## Acceptance Criteria":
			lines[i] = strings.Replace(line, "Acceptance Criteria", "Acceptance", 1)
		}
	}
	body = strings.Join(lines, "")
	deps := list(s.fields["dependencies"])
	if len(deps) > 0 {
		body += "\n## Depends on\n"
		for _, dep := range deps {
			ref := ids[canonical(dep)]
			if ref == "" {
				ref = dep
			}
			body += "- " + ref + "\n"
		}
	}
	raw := append(bytes.Clone(s.raw[:end]), []byte(body)...)
	d := task.Parse(task.BuildName("", id, task.Slug(s.title)), raw)
	status := str(s.fields["status"])
	switch status {
	case "To Do":
		status = "todo"
	case "In Progress":
		status = "doing"
	case "Done":
		status = "done"
	}
	typ, reason := "feature", "default: no explicit bug/docs/chore/spike label"
	for _, label := range list(s.fields["labels"]) {
		v := strings.ToLower(label)
		if v == "bug" || v == "docs" || v == "chore" || v == "spike" {
			typ = v
			reason = "label: " + label
			break
		}
	}
	// The old type remains available even when it is not a Huginn enum.
	updates := []struct {
		k string
		v any
	}{{"id", id}, {"aliases", []string{s.alias}}, {"status", status}, {"type", typ}, {"import_type_inference", reason}}
	if value, ok := s.fields["type"]; ok {
		updates = append(updates, struct {
			k string
			v any
		}{"backlog_type", value})
	}
	for _, pair := range [][2]string{{"created_date", "created"}, {"updated_date", "updated"}} {
		if v, ok := s.fields[pair[0]]; ok {
			updates = append(updates, struct {
				k string
				v any
			}{pair[1], str(v)})
		}
	}
	for _, u := range updates {
		if err := d.SetField(u.k, u.v); err != nil {
			return nil, err
		}
	}
	return d.Bytes(), nil
}

// units exposes raw field/section spans for conservative three-way merging.
// No YAML reserialization occurs, including for unknown nested fields.
type unit struct{ key, raw string }

func units(raw []byte) ([]unit, error) {
	_, end, err := front(raw)
	if err != nil {
		return nil, err
	}
	lines := strings.SplitAfter(string(raw), "\n")
	var node yaml.Node
	header := string(raw[:end])
	last := strings.LastIndex(strings.TrimSuffix(header, "\n"), "---")
	if err = yaml.Unmarshal([]byte(header[len(lines[0]):last]), &node); err != nil {
		return nil, err
	}
	offsets := []int{0}
	for _, line := range lines {
		offsets = append(offsets, offsets[len(offsets)-1]+len(line))
	}
	var out []unit
	mapping := node.Content[0]
	start := len(lines[0])
	out = append(out, unit{"open", string(raw[:start])})
	for i := 0; i < len(mapping.Content); i += 2 {
		a := offsets[mapping.Content[i].Line]
		if i == 0 && a > start {
			out = append(out, unit{"header", string(raw[start:a])})
		}
		b := last
		if i+2 < len(mapping.Content) {
			b = offsets[mapping.Content[i+2].Line]
		}
		out = append(out, unit{"field:" + mapping.Content[i].Value, string(raw[a:b])})
	}
	out = append(out, unit{"close", string(raw[last:end])})
	start = end
	key := "body"
	fence := ""
	counts := map[string]int{}
	for n, line := range lines {
		a := offsets[n]
		if a < end {
			continue
		}
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			if fence == "" {
				fence = trim[:3]
			} else if strings.HasPrefix(trim, fence) {
				fence = ""
			}
			continue
		}
		if fence == "" && strings.HasPrefix(line, "## ") {
			out = append(out, unit{key, string(raw[start:a])})
			key = "section:" + strings.TrimSpace(line[3:])
			counts[key]++
			key += fmt.Sprint(":", counts[key])
			start = a
		}
	}
	out = append(out, unit{key, string(raw[start:])})
	return out, nil
}
func merge(base, local, incoming []byte) ([]byte, error) {
	b, err := units(base)
	if err != nil {
		return nil, err
	}
	l, err := units(local)
	if err != nil {
		return local, nil
	}
	n, err := units(incoming)
	if err != nil {
		return nil, err
	}
	bm, lm, nm := map[string]string{}, map[string]string{}, map[string]string{}
	for _, u := range b {
		bm[u.key] = u.raw
	}
	for _, u := range l {
		lm[u.key] = u.raw
	}
	for _, u := range n {
		nm[u.key] = u.raw
	}
	var out strings.Builder
	// Follow local ordering, inserting newly introduced fields before closing YAML
	// and new sections at the end. Local deletions are edits and remain deleted.
	used := map[string]bool{}
	for _, u := range l {
		if u.key == "close" {
			for _, v := range n {
				if strings.HasPrefix(v.key, "field:") {
					_, was := bm[v.key]
					_, has := lm[v.key]
					if !was && !has {
						out.WriteString(v.raw)
						used[v.key] = true
					}
				}
			}
		}
		value := u.raw
		if previous, ok := bm[u.key]; ok && value == previous {
			value = nm[u.key]
		}
		out.WriteString(value)
		used[u.key] = true
	}
	for _, u := range n {
		if !used[u.key] {
			if _, was := bm[u.key]; !was && !strings.HasPrefix(u.key, "field:") {
				out.WriteString(u.raw)
			}
		}
	}
	return []byte(out.String()), nil
}
