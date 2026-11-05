package cli

import (
	"errors"
	"path/filepath"

	"github.com/veridit/huginn/internal/task"
)

// A combined remove/add is atomic even though the library exposes two methods.
func changeDependencies(store *task.Store, ref string, add, remove []string) (task.Result, error) {
	original, e := store.Get(ref)
	if e != nil {
		return task.Result{}, e
	}
	var result task.Result
	changed := false
	if len(remove) > 0 {
		result, e = store.RemoveDependencies(original.Task.ID, remove)
		changed = result.Changed
	}
	if e == nil && len(add) > 0 {
		result, e = store.AddDependencies(original.Task.ID, add)
		changed = changed || result.Changed
	}
	if e != nil && changed {
		rollback := task.AtomicWrite(filepath.Join(store.Root, filepath.FromSlash(original.Task.Path)), original.Bytes())
		if rollback == nil {
			_, rollback = store.Tasks()
		}
		return task.Result{}, errors.Join(e, rollback)
	}
	result.Changed = changed
	return result, e
}
