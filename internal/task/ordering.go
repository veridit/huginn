package task

import (
	"errors"
	"github.com/veridit/huginn/internal/rank"
	"path/filepath"
)

// Rename describes one pure filesystem rename, including automatic rank repair.
type Rename struct {
	ID   string `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
}
type rankChange struct {
	Task Task
	Rank string
}

func rankPlan(tasks []Task, moved Task, o MoveOptions) ([]rankChange, error) {
	if o.Before != "" && o.After != "" || o.Top && (o.Before != "" || o.After != "") {
		return nil, &Error{Kind: "conflicting-position", ID: moved.ID, Fix: "huginn move " + moved.ID + " --top"}
	}
	var ordered []Task
	for _, t := range tasks {
		if t.ID != moved.ID && t.Rank != "" {
			ordered = append(ordered, t)
		}
	}
	Sort(ordered)
	at := len(ordered)
	if o.Top {
		at = 0
	}
	ref := o.Before
	if ref == "" {
		ref = o.After
	}
	if ref != "" {
		target, e := Resolve(tasks, ref)
		if e != nil {
			return nil, e
		}
		if target.ID == moved.ID {
			return []rankChange{}, nil
		}
		for n, t := range ordered {
			if t.ID == target.ID {
				at = n
				if o.After != "" {
					at++
				}
				break
			}
		}
	}
	ordered = append(ordered, Task{})
	copy(ordered[at+1:], ordered[at:])
	ordered[at] = moved
	// Enumerating windows by cardinality guarantees the smallest contiguous
	// affected window. Among equal-size windows choose the leftmost one.
	for size := 1; size <= len(ordered); size++ {
		for start := max(0, at-size+1); start <= min(at, len(ordered)-size); start++ {
			end := start + size
			a, b := "", ""
			if start > 0 {
				a = ordered[start-1].Rank
			}
			if end < len(ordered) {
				b = ordered[end].Rank
			}
			keys, e := rank.Series(a, b, size, 12)
			if e != nil {
				continue
			}
			plan := make([]rankChange, 0, size)
			for n, key := range keys {
				t := ordered[start+n]
				if key != t.Rank || t.ID == moved.ID {
					plan = append(plan, rankChange{t, key})
				}
			}
			return plan, nil
		}
	}
	return nil, &Error{Kind: "rank-capacity", ID: moved.ID, Fix: "huginn lint"}
}
func (s *Store) applyRanks(plan []rankChange, focus Task) (Result, error) {
	result := Result{Task: focus, Next: hint(focus)}
	// Preflight all paths before changing the first file.
	for _, p := range plan {
		if p.Task.Path == "" {
			continue
		}
		dest := filepath.ToSlash(filepath.Join(p.Task.Group, BuildName(p.Rank, p.Task.ID, p.Task.Slug)))
		if dest == p.Task.Path {
			continue
		}
		path, e := s.diskPath(dest)
		if e != nil {
			return Result{}, e
		}
		source, e := s.diskPath(p.Task.Path)
		if e != nil {
			return Result{}, e
		}
		available, e := renameDestinationAvailable(source, path)
		if e != nil {
			return Result{}, e
		}
		if !available {
			return Result{}, &Error{Kind: "path-exists", ID: p.Task.ID, Value: dest, Fix: "huginn lint"}
		}
	}
	local := *s
	local.Refresh = nil
	applied := []rankChange{}
	for _, p := range plan {
		if p.Task.Path == "" {
			continue
		}
		d := &Document{Task: p.Task}
		r, e := local.rename(d, p.Task.Group, p.Rank, p.Task.Slug)
		if e != nil {
			var rollback error
			for n := len(applied) - 1; n >= 0; n-- {
				old := applied[n]
				current := old.Task
				current.Path = filepath.ToSlash(filepath.Join(current.Group, BuildName(old.Rank, current.ID, current.Slug)))
				_, re := local.rename(&Document{Task: current}, old.Task.Group, old.Task.Rank, old.Task.Slug)
				rollback = errors.Join(rollback, re)
			}
			return result, errors.Join(e, rollback)
		}
		if r.Changed {
			applied = append(applied, p)
			result.Renamed = append(result.Renamed, p.Task.ID)
			result.Renames = append(result.Renames, Rename{p.Task.ID, p.Task.Path, r.Task.Path})
			result.Changed = true
		}
		if p.Task.ID == focus.ID {
			result.Task = r.Task
			result.Next = hint(r.Task)
		}
	}
	if result.Changed && s.Refresh != nil {
		if _, e := s.Refresh(); e != nil {
			return result, e
		}
	}
	return result, nil
}

// Rebalance explicitly re-keys all ranked tasks, or the contiguous ranked
// window spanning refs. Unranked tasks are deliberately left unranked.
func (s *Store) Rebalance(refs []string) (Result, error) {
	tasks, e := s.Tasks()
	if e != nil {
		return Result{}, e
	}
	var ordered []Task
	for _, t := range tasks {
		if t.Rank != "" {
			ordered = append(ordered, t)
		}
	}
	Sort(ordered)
	start, end := 0, len(ordered)
	if len(refs) > 0 {
		start = len(ordered)
		end = 0
		for _, ref := range refs {
			target, e := Resolve(tasks, ref)
			if e != nil {
				return Result{}, e
			}
			for n, t := range ordered {
				if t.ID == target.ID {
					start = min(start, n)
					end = max(end, n+1)
				}
			}
		}
	}
	if end <= start {
		return Result{Next: "huginn next"}, nil
	}
	a, b := "", ""
	if start > 0 {
		a = ordered[start-1].Rank
	}
	if end < len(ordered) {
		b = ordered[end].Rank
	}
	keys, e := rank.Series(a, b, end-start, 12)
	// Explicit windows may be surrounded by dense human ranks. Grow as needed,
	// just as move does, rather than exposing a repair error to an agent.
	for e != nil && (start > 0 || end < len(ordered)) {
		if start > 0 {
			start--
		}
		if end < len(ordered) {
			end++
		}
		a, b = "", ""
		if start > 0 {
			a = ordered[start-1].Rank
		}
		if end < len(ordered) {
			b = ordered[end].Rank
		}
		keys, e = rank.Series(a, b, end-start, 12)
	}
	if e != nil {
		return Result{}, e
	}
	plan := make([]rankChange, 0, len(keys))
	for n, key := range keys {
		plan = append(plan, rankChange{ordered[start+n], key})
	}
	r, e := s.applyRanks(plan, ordered[start])
	r.Next = "huginn next"
	return r, e
}
