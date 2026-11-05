package task

import (
	"fmt"
	"github.com/veridit/huginn/internal/rank"
	"sort"
	"strings"
)

type Match struct {
	Slug string `json:"slug"`
	ID   string `json:"id"`
}
type Unchecked struct {
	Index int    `json:"index"`
	Text  string `json:"text"`
	Fix   string `json:"fix"`
}
type Error struct {
	Kind         string      `json:"kind"`
	ID           string      `json:"id,omitempty"`
	Field        string      `json:"field,omitempty"`
	Value        string      `json:"value,omitempty"`
	Closest      string      `json:"closest,omitempty"`
	Fix          string      `json:"fix,omitempty"`
	Valid        []string    `json:"valid,omitempty"`
	Matches      []Match     `json:"matches,omitempty"`
	Dependencies []Task      `json:"dependencies,omitempty"`
	Unchecked    []Unchecked `json:"unchecked,omitempty"`
}

func (e *Error) Error() string {
	msg := e.Kind
	if e.ID != "" {
		msg += " " + e.ID
	}
	if e.Field != "" {
		msg += " " + e.Field
	}
	if e.Value != "" {
		msg += ": " + e.Value
	}
	if len(e.Valid) > 0 {
		msg += "; valid: " + strings.Join(e.Valid, ", ") + "; closest: " + e.Closest
	}
	for _, m := range e.Matches {
		msg += "\n" + m.Slug + "  " + m.ID
	}
	for _, d := range e.Dependencies {
		msg += "\n" + d.ID + " " + d.Title
	}
	for _, a := range e.Unchecked {
		msg += fmt.Sprintf("\n%d. %s\nfix: %s", a.Index, a.Text, a.Fix)
	}
	if e.Fix != "" {
		msg += "\nfix: " + e.Fix
	}
	return msg
}
func (e *Error) ExitCode() int {
	switch e.Kind {
	case "blocked", "unchecked-acceptance":
		return 3
	case "nothing-matches":
		return 4
	default:
		return 2
	}
}
func Invalid(field, value string, valid []string) *Error {
	closest := ""
	best := int(^uint(0) >> 1)
	for _, s := range valid {
		n := distance(strings.ToLower(value), s)
		if n < best {
			best = n
			closest = s
		}
	}
	return &Error{Kind: "invalid-value", Field: field, Value: value, Valid: valid, Closest: closest, Fix: fmt.Sprintf("huginn set <id> --%s %s", field, closest)}
}
func invalidFor(id, field, value string, valid []string) *Error {
	e := Invalid(field, value, valid)
	e.ID = id
	e.Fix = strings.ReplaceAll(e.Fix, "<id>", id)
	if field == "field" {
		e.Fix = "huginn set " + id + " --type feature"
	}
	return e
}
func distance(a, b string) int {
	p := make([]int, len(b)+1)
	for j := range p {
		p[j] = j
	}
	for i := range a {
		q := make([]int, len(b)+1)
		q[0] = i + 1
		for j := range b {
			cost := 0
			if a[i] != b[j] {
				cost = 1
			}
			q[j+1] = min(q[j]+1, p[j+1]+1, p[j]+cost)
		}
		p = q
	}
	return p[len(b)]
}
func Resolve(tasks []Task, ref string) (Task, error) {
	ref = strings.ToLower(ref)
	var ids []Task
	for _, t := range tasks {
		if strings.EqualFold(t.ID, ref) {
			ids = append(ids, t)
		}
	}
	if len(ids) == 1 {
		return ids[0], nil
	}
	var exact, prefix []Task
	for _, t := range tasks {
		hit := strings.EqualFold(t.ID, ref) || strings.EqualFold(t.Slug, ref)
		for _, a := range t.Aliases {
			hit = hit || strings.EqualFold(a, ref)
		}
		if hit {
			exact = append(exact, t)
		} else if ref != "" && strings.HasPrefix(strings.ToLower(t.ID), ref) {
			prefix = append(prefix, t)
		}
	}
	if len(ids) > 1 {
		exact = ids
	}
	if len(exact) == 0 {
		exact = prefix
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	e := &Error{Kind: "not-found", Value: ref, Fix: "huginn ls"}
	if len(exact) > 1 {
		e.Kind = "ambiguous"
		sort.Slice(exact, func(i, j int) bool { return exact[i].ID < exact[j].ID })
		for _, t := range exact {
			e.Matches = append(e.Matches, Match{t.Slug, t.ID})
		}
	}
	return Task{}, e
}

// ResolveDependency deliberately accepts only exact ids and aliases, never slugs or prefixes.
func ResolveDependency(tasks []Task, ref string) (Task, error) {
	var ids []Task
	for _, t := range tasks {
		if strings.EqualFold(t.ID, ref) {
			ids = append(ids, t)
		}
	}
	if len(ids) == 1 {
		return ids[0], nil
	}
	if len(ids) > 1 {
		return Task{}, &Error{Kind: "duplicate-id", ID: ref, Fix: "huginn lint"}
	}
	var found []Task
	for _, t := range tasks {
		hit := strings.EqualFold(t.ID, ref)
		for _, a := range t.Aliases {
			hit = hit || strings.EqualFold(a, ref)
		}
		if hit {
			found = append(found, t)
		}
	}
	if len(found) == 1 {
		return found[0], nil
	}
	return Task{}, &Error{Kind: "dangling-dependency", Value: ref, Fix: "huginn lint"}
}
func Blocked(t Task, tasks []Task) []Task {
	var out []Task
	for _, d := range t.Dependencies {
		x, e := ResolveDependency(tasks, d.Ref)
		if e != nil {
			out = append(out, Task{ID: d.Ref, Title: "unresolved dependency"})
		} else if x.Status != "done" && x.Status != "dropped" {
			out = append(out, x)
		}
	}
	return out
}
func Sort(tasks []Task) {
	sort.Slice(tasks, func(i, j int) bool { return rank.Less(tasks[i].Rank, tasks[i].ID, tasks[j].Rank, tasks[j].ID) })
}
func Lint(docs []*Document) []Finding {
	var out []Finding
	var tasks []Task
	counts := map[string]int{}
	for _, d := range docs {
		counts[d.Task.ID]++
	}
	for _, d := range docs {
		out = append(out, d.Findings...)
		t := d.Task
		tasks = append(tasks, t)
		if t.ID != "" && counts[t.ID] > 1 {
			out = append(out, Finding{t.Path, 1, "duplicate id " + t.ID})
		}
	}
	adj := map[string][]string{}
	for _, t := range tasks {
		for _, dep := range t.Dependencies {
			v, e := ResolveDependency(tasks, dep.Ref)
			if e != nil {
				out = append(out, Finding{t.Path, dep.Line, "dangling dependency " + dep.Ref})
			} else {
				adj[t.ID] = append(adj[t.ID], v.ID)
			}
		}
	}
	for _, t := range tasks {
		visited := map[string]bool{}
		var walk func(string) bool
		walk = func(v string) bool {
			if visited[v] {
				return false
			}
			visited[v] = true
			for _, w := range adj[v] {
				if w == t.ID || walk(w) {
					return true
				}
			}
			return false
		}
		if walk(t.ID) {
			out = append(out, Finding{t.Path, 1, "dependency cycle involving " + t.ID})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Problem < out[j].Problem
	})
	return out
}
