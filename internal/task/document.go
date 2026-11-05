// Package task owns lossless Markdown documents and task operations.
package task

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/veridit/huginn/internal/id"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

var Statuses = []string{"todo", "doing", "done", "dropped"}
var Types = []string{"feature", "bug", "chore", "spike", "docs"}

type Finding struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Problem string `json:"problem"`
}

func (f Finding) String() string { return fmt.Sprintf("%s:%d: %s", f.Path, f.Line, f.Problem) }

type Dependency struct {
	Ref  string `json:"ref"`
	Line int    `json:"line"`
	Note string `json:"note,omitempty"`
}
type Acceptance struct {
	Text    string `json:"text"`
	Checked bool   `json:"checked"`
	Line    int    `json:"line"`
}
type Task struct {
	ID           string       `json:"id"`
	Path         string       `json:"path"`
	Group        string       `json:"group"`
	Rank         string       `json:"rank"`
	Slug         string       `json:"slug"`
	Title        string       `json:"title"`
	Status       string       `json:"status"`
	Type         string       `json:"type"`
	Priority     string       `json:"priority"`
	Created      string       `json:"created"`
	Updated      string       `json:"updated"`
	Assignee     []string     `json:"assignee"`
	Labels       []string     `json:"labels"`
	Aliases      []string     `json:"aliases"`
	Goal         string       `json:"goal"`
	Dependencies []Dependency `json:"dependencies"`
	Acceptance   []Acceptance `json:"acceptance"`
}
type Document struct {
	Task             Task
	raw              []byte
	lines            []string
	fields           map[string]int
	fieldEnds        map[string]int
	sections         map[string][2]int
	Findings         []Finding
	frontEnd         int
	frontmatterValid bool
	newline          string
}

func ParseName(path string) (r, i, s string, err error) {
	n := strings.TrimSuffix(filepath.Base(path), ".md")
	p := strings.Split(n, ".")
	switch len(p) {
	case 2:
		i, s = p[0], p[1]
	case 3:
		r, i, s = p[0], p[1], p[2]
	default:
		return "", "", "", fmt.Errorf("expected [rank.]id.slug.md")
	}
	if !strings.HasSuffix(path, ".md") || !id.Valid(i) || s == "" {
		err = fmt.Errorf("expected [rank.]id.slug.md with a six-character Crockford id")
	}
	return
}
func BuildName(r, i, s string) string {
	if r != "" {
		return r + "." + i + "." + s + ".md"
	}
	return i + "." + s + ".md"
}
func Slug(title string) string {
	words := strings.FieldsFunc(strings.ToLower(title), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(words) > 6 {
		words = words[:6]
	}
	if len(words) == 0 {
		return "task"
	}
	return strings.Join(words, "-")
}
func scalar(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "\"") {
		var v string
		if json.NewDecoder(strings.NewReader(s)).Decode(&v) == nil {
			return v
		}
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'")
	}
	if n := strings.Index(s, " #"); n >= 0 {
		s = s[:n]
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'")
	}
	return strings.TrimSpace(s)
}
func list(s string) []string {
	var out []string
	if json.Unmarshal([]byte(s), &out) == nil {
		return out
	}
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "[") {
		if n := strings.LastIndex(s, "]"); n >= 0 {
			s = s[1:n]
		}
	}
	for _, v := range strings.Split(s, ",") {
		if v = scalar(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
func Parse(path string, data []byte) *Document {
	d := &Document{raw: bytes.Clone(data), fields: map[string]int{}, fieldEnds: map[string]int{}, sections: map[string][2]int{}, frontEnd: -1, newline: "\n"}
	if bytes.Contains(data, []byte("\r\n")) {
		d.newline = "\r\n"
	}
	d.lines = strings.SplitAfter(string(data), "\n")
	d.Task.Path = path
	d.Task.Group = filepath.ToSlash(filepath.Dir(path))
	if d.Task.Group == "." {
		d.Task.Group = ""
	}
	r, i, s, e := ParseName(path)
	d.Task.Rank, d.Task.ID, d.Task.Slug = r, i, s
	if e != nil {
		d.add(1, e.Error())
	}
	clean := func(s string) string {
		return strings.ToValidUTF8(strings.TrimSuffix(strings.TrimSuffix(s, "\n"), "\r"), "\ufffd")
	}
	if len(d.lines) > 0 && clean(d.lines[0]) == "---" {
		for n := 1; n < len(d.lines); n++ {
			line := clean(d.lines[n])
			if line == "---" {
				d.frontEnd = n
				break
			}
		}
	}
	if d.frontEnd < 0 {
		d.add(1, "missing or unterminated frontmatter")
	} else {
		d.parseFrontmatter()
	}
	start := d.frontEnd + 1
	if start < 0 {
		start = 0
	}
	section := ""
	sectionStart := 0
	fence := ""
	seenSections := map[string]bool{}
	for n := start; n < len(d.lines); n++ {
		line := clean(d.lines[n])
		trimmed := strings.TrimLeft(line, " ")
		if len(line)-len(trimmed) <= 3 && (strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")) {
			if section == "Goal" {
				d.Task.Goal += d.lines[n]
			}
			if fence == "" {
				ch := trimmed[0]
				count := 0
				for count < len(trimmed) && trimmed[count] == ch {
					count++
				}
				fence = trimmed[:count]
			} else if strings.HasPrefix(trimmed, fence) && strings.TrimSpace(strings.TrimPrefix(trimmed, fence)) == "" {
				fence = ""
			}
			continue
		}
		if fence != "" {
			if section == "Goal" {
				d.Task.Goal += d.lines[n]
			}
			continue
		}
		if strings.HasPrefix(line, "## ") {
			if section != "" {
				d.sections[section] = [2]int{sectionStart, n}
			}
			section = strings.TrimSpace(line[3:])
			if seenSections[section] && contains([]string{"Goal", "Depends on", "Acceptance", "Notes"}, section) {
				d.add(n+1, "duplicate section "+section)
			}
			seenSections[section] = true
			sectionStart = n
			continue
		}
		switch section {
		case "Goal":
			d.Task.Goal += d.lines[n]
		case "Depends on":
			t := strings.TrimSpace(line)
			t = strings.TrimSpace(strings.TrimPrefix(t, "- "))
			p := strings.Fields(t)
			if len(p) > 0 {
				d.Task.Dependencies = append(d.Task.Dependencies, Dependency{p[0], n + 1, strings.TrimSpace(strings.TrimPrefix(t, p[0]))})
			}
		case "Acceptance":
			t := strings.TrimSpace(line)
			if strings.HasPrefix(t, "- [ ] ") || strings.HasPrefix(t, "- [x] ") || strings.HasPrefix(t, "- [X] ") {
				d.Task.Acceptance = append(d.Task.Acceptance, Acceptance{t[6:], t[3] != ' ', n + 1})
			}
		}
	}
	if section != "" {
		d.sections[section] = [2]int{sectionStart, len(d.lines)}
	}
	for _, field := range []struct{ k, v string }{{"id", d.Task.ID}, {"title", d.Task.Title}, {"status", d.Task.Status}} {
		_, ok := d.fields[field.k]
		if !ok || strings.TrimSpace(field.v) == "" {
			d.add(1, "missing field "+field.k)
		}
	}
	if !id.Valid(d.Task.ID) {
		d.add(d.fieldLine("id"), "invalid id")
	}
	if i != "" && i != d.Task.ID {
		d.add(1, "filename id differs from frontmatter id")
	}
	if !contains(Statuses, d.Task.Status) {
		d.add(d.fieldLine("status"), "unknown status "+d.Task.Status)
	}
	if d.Task.Type != "" && !contains(Types, d.Task.Type) {
		d.add(d.fieldLine("type"), "unknown type "+d.Task.Type)
	}
	if _, ok := d.sections["Goal"]; !ok || strings.TrimSpace(d.Task.Goal) == "" {
		d.add(1, "missing Goal")
	}
	if len(d.Task.Acceptance) == 0 {
		d.add(1, "Acceptance requires at least one item")
	}
	if d.Task.Status == "done" {
		for _, a := range d.Task.Acceptance {
			if !a.Checked {
				d.add(a.Line, "done with unchecked acceptance")
			}
		}
	}
	d.Task.Goal = strings.ToValidUTF8(d.Task.Goal, "\ufffd")
	return d
}
func (d *Document) add(line int, p string) {
	d.Findings = append(d.Findings, Finding{d.Task.Path, line, p})
}
func (d *Document) fieldLine(k string) int {
	if n, ok := d.fields[k]; ok {
		return n + 1
	}
	return 1
}
func contains(v []string, s string) bool {
	for _, x := range v {
		if x == s {
			return true
		}
	}
	return false
}
func (d *Document) Bytes() []byte { return bytes.Clone(d.raw) }
func (d *Document) replace(lines []string) {
	fresh := Parse(d.Task.Path, []byte(strings.Join(lines, "")))
	*d = *fresh
}
func (d *Document) SetField(k string, value any) error {
	if d.frontEnd < 0 || !d.frontmatterValid {
		return &Error{Kind: "broken-file", ID: d.Task.ID, Fix: "huginn lint"}
	}
	b, e := json.Marshal(value)
	if e != nil {
		return e
	}
	lines := append([]string(nil), d.lines...)
	line := k + ": " + string(b) + d.newline
	if n, ok := d.fields[k]; ok {
		end := d.fieldEnds[k]
		if end <= n {
			end = n + 1
		}
		lines = append(lines[:n], append([]string{line}, lines[end:]...)...)
	} else {
		n := d.frontEnd
		lines = append(lines[:n], append([]string{line}, lines[n:]...)...)
	}
	d.replace(lines)
	return nil
}
func (d *Document) Check(n int) error {
	if n < 1 || n > len(d.Task.Acceptance) {
		return &Error{Kind: "acceptance-index", ID: d.Task.ID, Value: fmt.Sprint(n), Fix: fmt.Sprintf("huginn show %s", d.Task.ID)}
	}
	a := d.Task.Acceptance[n-1]
	if a.Checked {
		return nil
	}
	lines := append([]string(nil), d.lines...)
	lines[a.Line-1] = strings.Replace(lines[a.Line-1], "[ ]", "[x]", 1)
	d.replace(lines)
	return nil
}
func (d *Document) Note(text string) {
	lines := append([]string(nil), d.lines...)
	s, ok := d.sections["Notes"]
	if !ok {
		if len(lines) > 0 && !strings.HasSuffix(lines[len(lines)-1], "\n") {
			lines = append(lines, d.newline)
		}
		lines = append(lines, d.newline+"## Notes"+d.newline+text+d.newline)
	} else {
		n := s[1]
		insert := text + d.newline
		if n > 0 && !strings.HasSuffix(lines[n-1], "\n") {
			insert = d.newline + insert
		}
		lines = append(lines[:n], append([]string{insert}, lines[n:]...)...)
	}
	d.replace(lines)
}
func AtomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".huginn-*")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	mode := os.FileMode(0644)
	if st, e := os.Stat(path); e == nil {
		mode = st.Mode().Perm()
	}
	if e = f.Chmod(mode); e == nil {
		_, e = f.Write(data)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	return os.Rename(tmp, path)
}
