package task

import (
	"errors"
	"strings"
)

// AddDependencies resolves refs once to canonical ids and adds only missing
// edges. Existing source lines, including aliases and human notes, are untouched.
// A proposed cycle rejects the entire operation before any file is written.
func (s *Store) AddDependencies(ref string, refs []string) (Result, error) {
	d, tasks, err := s.load(ref)
	if err != nil {
		return Result{}, err
	}
	present := map[string]bool{}
	for _, dep := range d.Task.Dependencies {
		if target, err := ResolveDependency(tasks, dep.Ref); err == nil {
			present[target.ID] = true
		}
	}
	var added []string
	for _, ref := range refs {
		target, err := Resolve(tasks, ref)
		if err != nil {
			return Result{}, err
		}
		if present[target.ID] {
			continue
		}
		if path := dependencyPath(tasks, target.ID, d.Task.ID); len(path) > 0 {
			cycle := append([]string{d.Task.ID}, path...)
			// Prefer an existing edge as the repair, since the proposed edge
			// has not been written. Self-dependency has no existing edge.
			from, to := d.Task.ID, target.ID
			if len(path) > 1 {
				from, to = path[0], path[1]
			}
			var involved []Task
			seen := map[string]bool{}
			for _, id := range cycle {
				if seen[id] {
					continue
				}
				seen[id] = true
				for _, task := range tasks {
					if task.ID == id {
						involved = append(involved, task)
						break
					}
				}
			}
			return Result{}, &Error{Kind: "blocked", ID: d.Task.ID, Field: "dependencies", Value: "dependency cycle: " + strings.Join(cycle, " -> "), Cycle: cycle, Dependencies: involved, Fix: "huginn dep " + from + " --remove " + to}
		}
		present[target.ID] = true
		added = append(added, target.ID)
	}
	if len(added) == 0 {
		return s.finish(d, false)
	}
	d.appendDependencies(added)
	for _, id := range added {
		found := false
		for _, dep := range d.Task.Dependencies {
			found = found || dep.Ref == id
		}
		if !found {
			return Result{}, &Error{Kind: "broken-file", ID: d.Task.ID, Field: "dependencies", Fix: "huginn lint"}
		}
	}
	return s.finish(d, true)
}

// RemoveDependencies removes all matching source lines without rewriting any
// remaining text. A dangling source token can also be removed by its exact
// spelling (case-insensitively), so deleting a target never prevents repair.
func (s *Store) RemoveDependencies(ref string, refs []string) (Result, error) {
	d, tasks, err := s.load(ref)
	if err != nil {
		return Result{}, err
	}
	ids, raw := map[string]bool{}, map[string]bool{}
	for _, ref := range refs {
		target, err := Resolve(tasks, ref)
		if err == nil {
			ids[target.ID] = true
			continue
		}
		var typed *Error
		if !errors.As(err, &typed) || typed.Kind != "not-found" {
			return Result{}, err
		}
		// Missing targets are safe no-ops when absent, and exact dangling
		// tokens are removable. Never guess an ambiguous resolver match.
		raw[strings.ToLower(ref)] = true
	}
	remove := map[int]bool{}
	for _, dep := range d.Task.Dependencies {
		matched := raw[strings.ToLower(dep.Ref)]
		if target, err := ResolveDependency(tasks, dep.Ref); err == nil {
			matched = matched || ids[target.ID]
		}
		if matched {
			remove[dep.Line-1] = true
		}
	}
	if len(remove) == 0 {
		return s.finish(d, false)
	}
	lines := make([]string, 0, len(d.lines)-len(remove))
	for n, line := range d.lines {
		if !remove[n] {
			lines = append(lines, line)
		}
	}
	d.replace(lines)
	return s.finish(d, true)
}

func (d *Document) appendDependencies(ids []string) {
	lines := append([]string(nil), d.lines...)
	var text strings.Builder
	section, exists := d.sections["Depends on"]
	at := len(lines)
	if exists {
		at = section[1]
	}
	if at > 0 && lines[at-1] != "" && !strings.HasSuffix(lines[at-1], "\n") {
		text.WriteString(d.newline)
	}
	if !exists {
		text.WriteString(d.newline + "## Depends on" + d.newline)
	}
	for _, id := range ids {
		text.WriteString("- " + id + d.newline)
	}
	lines = append(lines[:at], append([]string{text.String()}, lines[at:]...)...)
	d.replace(lines)
}

// dependencyPath returns an actual canonical-id path from start to goal.
// Existing dangling/ambiguous edges are left for lint, not guessed here.
func dependencyPath(tasks []Task, start, goal string) []string {
	byID := map[string]Task{}
	for _, task := range tasks {
		byID[task.ID] = task
	}
	visited := map[string]bool{}
	var walk func(string) []string
	walk = func(id string) []string {
		if id == goal {
			return []string{id}
		}
		if visited[id] {
			return nil
		}
		visited[id] = true
		for _, dep := range byID[id].Dependencies {
			if target, err := ResolveDependency(tasks, dep.Ref); err == nil {
				if path := walk(target.ID); len(path) > 0 {
					return append([]string{id}, path...)
				}
			}
		}
		return nil
	}
	return walk(start)
}
