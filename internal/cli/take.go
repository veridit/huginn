package cli

import (
	"context"
	"errors"
	"os/exec"
	"strings"

	"github.com/veridit/huginn/gitbranch"
	"github.com/veridit/huginn/internal/task"
)

// take is the one push-lock path, shared by CLI, MCP and the TUI callback.
func (a *App) take(ctx context.Context, s *session, ref string) (task.Result, error) {
	user, e := a.identity(ctx)
	if e != nil {
		return task.Result{}, e
	}
	// Publish unrelated local history before gitbranch's lost-race rollback.
	// Otherwise an unpublished new task could disappear along with a losing take.
	if s.r.Remote != "" {
		ahead := exec.CommandContext(ctx, "git", "-C", s.r.Dir, "rev-list", "--count", "refs/remotes/"+s.r.Remote+"/huginn..HEAD")
		count, err := ahead.Output()
		if err != nil || strings.TrimSpace(string(count)) != "0" {
			if err = s.r.Sync(); err != nil {
				return task.Result{}, err
			}
		}
	}
	var result task.Result
	e = s.r.Update(func(_ *gitbranch.Repository) error {
		// Update has pulled already. Read authoritative files without invoking Git
		// inside its callback, then refresh the disposable cache after the lock.
		local := *s.s
		local.Refresh = nil
		var err error
		result, err = local.Take(ref, user)
		return err
	})
	var raced *gitbranch.ErrRaced
	if errors.As(e, &raced) {
		d, err := s.s.Get(ref)
		if err != nil {
			return task.Result{}, err
		}
		return task.Result{}, &OperationError{Problem: "taken by " + strings.Join(d.Task.Assignee, ", ") + " (" + d.Task.ID + ")", Fix: "huginn next"}
	}
	if e == nil {
		_, e = s.s.Tasks()
	}
	return result, e
}
