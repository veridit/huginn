package task

import (
	"fmt"
	"github.com/veridit/huginn/internal/id"
	"github.com/veridit/huginn/internal/rank"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Result struct {
	Task     Task
	Changed  bool
	Next     string
	Warnings []string
}
type NewOptions struct {
	Title, Goal, Group, Type, Priority string
	Acceptance, Deps, Labels           []string
	Top                                bool
	After                              string
}
type Filter struct {
	Type, Priority, Label, Group, Mine string
	Status                             string
}
type MoveOptions struct {
	Before, After string
	Top           bool
}

// Store exposes filesystem operations. Cache integration is supplied via Refresh,
// which must sync and return the current tree, before and after each operation.
type Store struct {
	Root     string
	DepsMode string
	Refresh  func() ([]Task, error)
}

func (s *Store) Tasks() ([]Task, error) {
	if s.Refresh != nil {
		return s.Refresh()
	}
	var out []Task
	e := filepath.WalkDir(s.Root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && path != s.Root {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(s.Root, path)
		if e != nil {
			return e
		}
		out = append(out, Parse(filepath.ToSlash(rel), b).Task)
		return nil
	})
	Sort(out)
	return out, e
}
func (s *Store) load(ref string) (*Document, []Task, error) {
	ts, e := s.Tasks()
	if e != nil {
		return nil, nil, e
	}
	t, e := Resolve(ts, ref)
	if e != nil {
		return nil, nil, e
	}
	b, e := os.ReadFile(filepath.Join(s.Root, t.Path))
	if e != nil {
		return nil, nil, e
	}
	return Parse(t.Path, b), ts, nil
}
func hint(t Task) string {
	switch t.Status {
	case "todo":
		return "huginn take " + t.ID
	case "doing":
		for n, a := range t.Acceptance {
			if !a.Checked {
				return fmt.Sprintf("huginn check %s %d", t.ID, n+1)
			}
		}
		return "huginn done " + t.ID
	default:
		return "huginn next"
	}
}
func (s *Store) finish(d *Document, changed bool) (Result, error) {
	if changed {
		if e := AtomicWrite(filepath.Join(s.Root, d.Task.Path), d.Bytes()); e != nil {
			return Result{}, e
		}
		if s.Refresh != nil {
			if _, e := s.Refresh(); e != nil {
				return Result{}, e
			}
		}
	}
	return Result{Task: d.Task, Changed: changed, Next: hint(d.Task)}, nil
}
func safeGroup(g string) error {
	if filepath.IsAbs(g) || g == ".." || strings.HasPrefix(filepath.Clean(g), ".."+string(filepath.Separator)) || strings.Contains(g, "\\") {
		return &Error{Kind: "invalid-group", Value: g, Fix: "huginn mv <id> auth"}
	}
	for _, p := range strings.Split(filepath.ToSlash(g), "/") {
		if strings.HasPrefix(p, ".") && p != "" && p != "." {
			return &Error{Kind: "invalid-group", Value: g, Fix: "huginn mv <id> auth"}
		}
	}
	return nil
}
func (s *Store) New(o NewOptions) (Result, error) {
	if strings.TrimSpace(o.Title) == "" || strings.TrimSpace(o.Goal) == "" || len(o.Acceptance) == 0 {
		return Result{}, &Error{Kind: "missing-required", Fix: "huginn new \"title\" --goal \"outcome\" --accept \"observable check\""}
	}
	if e := safeGroup(o.Group); e != nil {
		return Result{}, e
	}
	if o.Type == "" {
		o.Type = "feature"
	}
	if !contains(Types, o.Type) {
		return Result{}, Invalid("type", o.Type, Types)
	}
	ts, e := s.Tasks()
	if e != nil {
		return Result{}, e
	}
	known := map[string]bool{}
	for _, t := range ts {
		known[t.ID] = true
	}
	i, e := id.New(known)
	if e != nil {
		return Result{}, e
	}
	deps := []string{}
	for _, r := range o.Deps {
		t, e := Resolve(ts, r)
		if e != nil {
			return Result{}, e
		}
		deps = append(deps, t.ID)
	}
	r, e := position(ts, "", MoveOptions{Top: o.Top, After: o.After})
	if e != nil {
		return Result{}, e
	}
	slug := Slug(o.Title)
	path := filepath.ToSlash(filepath.Join(o.Group, BuildName(r, i, slug)))
	q := func(s string) string { return fmt.Sprintf("%q", s) }
	today := time.Now().Format("2006-01-02")
	body := "---\nid: " + i + "\ntitle: " + q(o.Title) + "\nstatus: todo\ntype: " + o.Type + "\npriority: " + q(o.Priority) + "\nassignee: []\nlabels: "
	labels := []string{}
	for _, l := range o.Labels {
		labels = append(labels, q(l))
	}
	body += "[" + strings.Join(labels, ", ") + "]\ncreated: " + today + "\nupdated: " + today + "\n---\n\n## Goal\n" + o.Goal + "\n\n## Depends on\n"
	for _, dep := range deps {
		body += "- " + dep + "\n"
	}
	body += "\n## Acceptance\n"
	for _, a := range o.Acceptance {
		body += "- [ ] " + a + "\n"
	}
	body += "\n## Notes\n"
	d := Parse(path, []byte(body))
	return s.finish(d, true)
}
func (s *Store) Set(ref, field, value string) (Result, error) {
	if field == "status" {
		return s.Transition(ref, value, "")
	}
	valid := []string{"title", "type", "priority", "label", "assignee"}
	if !contains(valid, field) {
		return Result{}, Invalid("field", field, valid)
	}
	if field == "type" && !contains(Types, value) {
		return Result{}, Invalid("type", value, Types)
	}
	d, _, e := s.load(ref)
	if e != nil {
		return Result{}, e
	}
	before := string(d.Bytes())
	var v any = value
	if field == "label" {
		field = "labels"
		v = []string{value}
	}
	if field == "assignee" {
		v = []string{value}
	}
	if e = d.SetField(field, v); e != nil {
		return Result{}, e
	}
	return s.finish(d, before != string(d.Bytes()))
}
func (s *Store) Check(ref string, n int) (Result, error) {
	d, _, e := s.load(ref)
	if e != nil {
		return Result{}, e
	}
	before := string(d.Bytes())
	if e = d.Check(n); e != nil {
		return Result{}, e
	}
	return s.finish(d, before != string(d.Bytes()))
}
func (s *Store) Note(ref, text string) (Result, error) {
	d, _, e := s.load(ref)
	if e != nil {
		return Result{}, e
	}
	d.Note(text)
	return s.finish(d, true)
}
func (s *Store) Take(ref, user string) (Result, error) { return s.Transition(ref, "doing", user) }
func (s *Store) Done(ref string) (Result, error)       { return s.Transition(ref, "done", "") }
func (s *Store) Drop(ref string) (Result, error)       { return s.Transition(ref, "dropped", "") }
func (s *Store) Transition(ref, status, user string) (Result, error) {
	if !contains(Statuses, status) {
		return Result{}, Invalid("status", status, Statuses)
	}
	d, ts, e := s.load(ref)
	if e != nil {
		return Result{}, e
	}
	if d.Task.Status == status && (status != "doing" || user == "" || contains(d.Task.Assignee, user)) {
		return s.finish(d, false)
	}
	if status == "doing" && user != "" && len(d.Task.Assignee) > 0 && !contains(d.Task.Assignee, user) {
		return Result{}, &Error{Kind: "taken", ID: d.Task.ID, Value: strings.Join(d.Task.Assignee, ", "), Fix: "huginn next"}
	}
	var warnings []string
	if status == "doing" || status == "done" {
		blocked := Blocked(d.Task, ts)
		if len(blocked) > 0 {
			if s.DepsMode != "warn" {
				return Result{}, &Error{Kind: "blocked", ID: d.Task.ID, Dependencies: blocked, Fix: "huginn show " + blocked[0].ID}
			}
			warnings = append(warnings, "open dependencies")
		}
	}
	if e = d.SetField("status", status); e != nil {
		return Result{}, e
	}
	if status == "doing" && user != "" {
		if e = d.SetField("assignee", []string{user}); e != nil {
			return Result{}, e
		}
	}
	if status == "done" {
		for _, a := range d.Task.Acceptance {
			if !a.Checked {
				warnings = append(warnings, "unchecked acceptance: "+a.Text)
			}
		}
	}
	r, e := s.finish(d, true)
	r.Warnings = warnings
	return r, e
}
func Next(tasks []Task, f Filter) (Result, error) {
	if f.Type != "" && !contains(Types, f.Type) {
		return Result{}, Invalid("type", f.Type, Types)
	}
	ts := append([]Task(nil), tasks...)
	Sort(ts)
	for _, t := range ts {
		if t.Status != "todo" || len(Blocked(t, tasks)) > 0 {
			continue
		}
		if f.Mine != "" {
			if !contains(t.Assignee, f.Mine) {
				continue
			}
		} else if len(t.Assignee) > 0 {
			continue
		}
		if f.Type != "" && t.Type != f.Type || f.Priority != "" && t.Priority != f.Priority || f.Label != "" && !contains(t.Labels, f.Label) || f.Group != "" && t.Group != f.Group {
			continue
		}
		return Result{Task: t, Next: hint(t)}, nil
	}
	return Result{}, &Error{Kind: "nothing-matches", Fix: "huginn ls"}
}
func position(ts []Task, skip string, o MoveOptions) (string, error) {
	if (o.Before != "" && o.After != "") || (o.Top && (o.Before != "" || o.After != "")) {
		return "", &Error{Kind: "conflicting-position", Fix: "huginn move <id> --top"}
	}
	var sorted []Task
	for _, t := range ts {
		if t.ID != skip {
			sorted = append(sorted, t)
		}
	}
	Sort(sorted)
	at := len(sorted)
	if o.Top {
		at = 0
	}
	ref := o.Before
	if ref == "" {
		ref = o.After
	}
	if ref != "" {
		target, e := Resolve(ts, ref)
		if e != nil {
			return "", e
		}
		if target.ID == skip {
			return "", &Error{Kind: "self-position", ID: skip, Fix: "huginn move " + skip + " --top"}
		}
		for n, t := range sorted {
			if t.ID == target.ID {
				at = n
				if o.After != "" {
					at++
				}
				break
			}
		}
	}
	a, b := "", ""
	if at > 0 {
		a = sorted[at-1].Rank
	}
	if at < len(sorted) {
		b = sorted[at].Rank
	}
	r, e := rank.Between(a, b)
	if e == nil && rank.NeedsRebalance(r) {
		return "", &Error{Kind: "rebalance-required", Fix: "huginn rebalance"}
	}
	return r, e
}
func (s *Store) rename(d *Document, group, r, slug string) (Result, error) {
	if e := safeGroup(group); e != nil {
		return Result{}, e
	}
	if slug == "" || strings.ContainsAny(slug, "/\\.\x00") {
		return Result{}, &Error{Kind: "invalid-slug", Value: slug, Fix: "huginn show " + d.Task.ID}
	}
	path := filepath.ToSlash(filepath.Join(group, BuildName(r, d.Task.ID, slug)))
	if path == d.Task.Path {
		return s.finish(d, false)
	}
	dest := filepath.Join(s.Root, path)
	if _, e := os.Lstat(dest); e == nil {
		return Result{}, &Error{Kind: "path-exists", ID: d.Task.ID, Value: path, Fix: "huginn lint"}
	} else if !os.IsNotExist(e) {
		return Result{}, e
	}
	if e := os.MkdirAll(filepath.Dir(dest), 0755); e != nil {
		return Result{}, e
	}
	if e := os.Rename(filepath.Join(s.Root, d.Task.Path), dest); e != nil {
		return Result{}, e
	}
	d.Task.Path = path
	d.Task.Group = group
	d.Task.Rank = r
	d.Task.Slug = slug
	if s.Refresh != nil {
		if _, e := s.Refresh(); e != nil {
			return Result{}, e
		}
	}
	return Result{Task: d.Task, Changed: true, Next: hint(d.Task)}, nil
}
func (s *Store) Move(ref string, o MoveOptions) (Result, error) {
	d, ts, e := s.load(ref)
	if e != nil {
		return Result{}, e
	}
	r, e := position(ts, d.Task.ID, o)
	if e != nil {
		return Result{}, e
	}
	return s.rename(d, d.Task.Group, r, d.Task.Slug)
}
func (s *Store) MoveGroup(ref, group string) (Result, error) {
	d, _, e := s.load(ref)
	if e != nil {
		return Result{}, e
	}
	return s.rename(d, group, d.Task.Rank, d.Task.Slug)
}
func (s *Store) Reslug(ref, slug string) (Result, error) {
	d, _, e := s.load(ref)
	if e != nil {
		return Result{}, e
	}
	return s.rename(d, d.Task.Group, d.Task.Rank, slug)
}
