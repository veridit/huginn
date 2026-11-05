package cache

import (
	"reflect"
	"strings"
)

func canonicalizer(files map[string]record) func(string) (string, bool) {
	ids := map[string][]string{}
	aliases := map[string][]string{}
	for _, r := range files {
		t := r.Task
		key := strings.ToLower(t.ID)
		ids[key] = append(ids[key], t.ID)
		seen := map[string]bool{}
		for _, a := range t.Aliases {
			key = strings.ToLower(a)
			if !seen[key] {
				aliases[key] = append(aliases[key], t.ID)
				seen[key] = true
			}
		}
	}
	return func(ref string) (string, bool) {
		key := strings.ToLower(ref)
		if m := ids[key]; len(m) == 1 {
			return m[0], true
		} else if len(m) > 1 {
			return ref, false
		}
		if m := aliases[key]; len(m) == 1 {
			return m[0], true
		}
		return ref, false
	}
}
func affectedReferences(old, next map[string]record, renames map[string]string) map[string]string {
	affected := map[string]string{}
	add := func(r record) {
		affected[strings.ToLower(r.Task.ID)] = r.Task.ID
		for _, a := range r.Task.Aliases {
			affected[strings.ToLower(a)] = a
		}
	}
	renamedNew := map[string]bool{}
	for _, p := range renames {
		renamedNew[p] = true
	}
	for p, prior := range old {
		if _, ok := renames[p]; ok {
			continue
		}
		current, ok := next[p]
		if !ok || current.Task.ID != prior.Task.ID || !reflect.DeepEqual(current.Task.Aliases, prior.Task.Aliases) {
			add(prior)
			if ok {
				add(current)
			}
		}
	}
	for p, r := range next {
		if _, ok := old[p]; !ok && !renamedNew[p] {
			add(r)
		}
	}
	return affected
}
