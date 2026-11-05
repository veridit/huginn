// Package gitbranch stores independent data on a parameterized orphan branch.
// It shells out to Git >= 2.42. With no remote it operates locally: Sync and
// Update still commit, but cannot provide cross-clone exclusion. A Repository
// must not be used concurrently. Separate clones coordinate through push.
package gitbranch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Identity overrides Git's configured author and committer for this handle.
type Identity struct{ Name, Email string }
type Option func(*Repository)

func WithIdentity(name, email string) Option {
	return func(r *Repository) { r.Identity = Identity{name, email} }
}

// Repository is a branch worktree. Dir is absolute. Remote is empty in local-only mode.
type Repository struct {
	Root, Dir, Branch, Remote string
	Identity                  Identity
}

// Error includes a copyable repair command and the original Git diagnostic.
type Error struct {
	Op, Detail, Fix string
	Cause           error
}

func (e *Error) Error() string { return e.Op + ": " + e.Detail + "\nfix: " + e.Fix }
func (e *Error) Unwrap() error { return e.Cause }

// ConflictError reports files that conflicted. The rebase has been aborted.
type ConflictError struct {
	Files []string
	Fix   string
}

func (e *ConflictError) Error() string {
	return "sync conflicts: " + strings.Join(e.Files, ", ") + " (rebase aborted)\nfix: " + e.Fix
}

// ErrRaced means another clone won the push. Dir has been reset to RemoteHead,
// so the caller can read the winner's files before reporting its assignee.
type ErrRaced struct{ RemoteHead, Dir, Fix string }

func (e *ErrRaced) Error() string {
	return "update lost push race; remote state restored at " + e.RemoteHead + "\nfix: " + e.Fix
}
func quote(s string) string {
	return "\"" + strings.ReplaceAll(strings.ReplaceAll(s, "\\", "\\\\"), "\"", "\\\"") + "\""
}
func (r *Repository) fix(args string) string { return "git -C " + quote(r.Dir) + " " + args }
func (r *Repository) runAt(dir string, args ...string) (string, error) {
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	c.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=true", "GIT_MERGE_AUTOEDIT=no")
	if r.Identity.Name != "" {
		c.Env = append(c.Env, "GIT_AUTHOR_NAME="+r.Identity.Name, "GIT_COMMITTER_NAME="+r.Identity.Name)
	}
	if r.Identity.Email != "" {
		c.Env = append(c.Env, "GIT_AUTHOR_EMAIL="+r.Identity.Email, "GIT_COMMITTER_EMAIL="+r.Identity.Email)
	}
	b, err := c.CombinedOutput()
	if err != nil {
		return string(b), &Error{Op: strings.Join(args, " "), Detail: strings.TrimSpace(string(b)), Fix: r.fix(strings.Join(args, " ")), Cause: err}
	}
	return strings.TrimSuffix(string(b), "\n"), nil
}
func (r *Repository) git(args ...string) (string, error) { return r.runAt(r.Dir, args...) }

// Open opens or initializes the worktree. repoRoot may be any directory inside
// the host checkout, including a linked checkout. dir is relative to its root.
func Open(repoRoot, branch, dir string, options ...Option) (*Repository, error) {
	return Init(repoRoot, branch, dir, options...)
}
func Init(repoRoot, branch, dir string, options ...Option) (*Repository, error) {
	r := &Repository{Dir: repoRoot}
	for _, o := range options {
		o(r)
	}
	version, err := r.runAt(repoRoot, "--version")
	if err != nil {
		return nil, err
	}
	var major, minor int
	fmt.Sscanf(version, "git version %d.%d", &major, &minor)
	if major < 2 || major == 2 && minor < 42 {
		return nil, &Error{Op: "init", Detail: "Git >= 2.42 required for worktree add --orphan; found " + version, Fix: "Install Git >= 2.42, then run git --version"}
	}
	root, err := r.runAt(repoRoot, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	r.Root = root
	clean := filepath.Clean(dir)
	if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, &Error{Op: "init", Detail: "worktree directory must be inside checkout", Fix: "Choose a relative directory such as .notes"}
	}
	if _, err = r.runAt(root, "check-ref-format", "--branch", branch); err != nil {
		return nil, err
	}
	r.Branch = branch
	r.Dir = filepath.Join(root, clean)
	remotes, err := r.runAt(root, "remote")
	if err != nil {
		return nil, err
	}
	for _, v := range strings.Fields(remotes) {
		if r.Remote == "" || v == "origin" {
			r.Remote = v
		}
		if v == "origin" {
			break
		}
	}
	common, err := r.runAt(root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	exclude := filepath.Join(common, "info", "exclude")
	if err = os.MkdirAll(filepath.Dir(exclude), 0755); err != nil {
		return nil, r.ioerr(err)
	}
	b, err := os.ReadFile(exclude)
	if err != nil && !os.IsNotExist(err) {
		return nil, r.ioerr(err)
	}
	entry := "/" + filepath.ToSlash(clean) + "/"
	found := false
	for _, line := range strings.Split(string(b), "\n") {
		if line == entry {
			found = true
		}
	}
	if !found {
		f, e := os.OpenFile(exclude, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if e != nil {
			return nil, r.ioerr(e)
		}
		prefix := ""
		if len(b) > 0 && b[len(b)-1] != '\n' {
			prefix = "\n"
		}
		_, e = f.WriteString(prefix + entry + "\n")
		ce := f.Close()
		if e != nil {
			return nil, r.ioerr(e)
		}
		if ce != nil {
			return nil, r.ioerr(ce)
		}
	}
	if _, e := os.Stat(filepath.Join(r.Dir, ".git")); e == nil {
		actual, e := r.git("symbolic-ref", "--short", "HEAD")
		if e != nil {
			return nil, e
		}
		if actual != branch {
			return nil, &Error{Op: "open", Detail: "existing worktree uses branch " + actual, Fix: r.fix("switch " + quote(branch))}
		}
		return r, nil
	}
	exists := false
	if _, e := r.runAt(root, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); e == nil {
		exists = true
	}
	remoteExists := false
	if r.Remote != "" {
		out, e := r.runAt(root, "ls-remote", "--heads", r.Remote, "refs/heads/"+branch)
		if e != nil {
			return nil, e
		}
		remoteExists = out != ""
		if remoteExists {
			if _, e = r.runAt(root, "fetch", r.Remote, "+refs/heads/"+branch+":refs/remotes/"+r.Remote+"/"+branch); e != nil {
				return nil, e
			}
		}
	}
	if exists {
		_, err = r.runAt(root, "worktree", "add", r.Dir, branch)
	} else if remoteExists {
		_, err = r.runAt(root, "worktree", "add", "-b", branch, r.Dir, "refs/remotes/"+r.Remote+"/"+branch)
	} else {
		_, err = r.runAt(root, "worktree", "add", "--orphan", "-b", branch, r.Dir)
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}
func (r *Repository) ioerr(e error) error {
	return &Error{Op: "filesystem", Detail: e.Error(), Fix: r.fix("status"), Cause: e}
}

// Head returns an empty string for an unborn branch.
func (r *Repository) Head() (string, error) {
	h, e := r.git("rev-parse", "--verify", "HEAD")
	if e != nil {
		if _, s := os.Stat(filepath.Join(r.Dir, ".git")); s == nil {
			if _, x := r.git("show-ref", "--verify", "refs/heads/"+r.Branch); x != nil {
				return "", nil
			}
		}
	}
	return h, e
}

// Change uses Git status letters (A/M/D/R...), including rename similarity.
type Change struct{ Status, OldPath, Path string }

func (r *Repository) DiffNameStatus(old, new string) ([]Change, error) {
	args := []string{"diff", "--name-status", "-z", "--find-renames", old, new, "--"}
	if old == "" {
		args = []string{"diff-tree", "--root", "--no-commit-id", "-r", "--name-status", "-z", "--find-renames", new, "--"}
	}
	out, e := r.git(args...)
	if e != nil {
		return nil, e
	}
	parts := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	var result []Change
	for i := 0; i+1 < len(parts); {
		c := Change{Status: parts[i], Path: parts[i+1]}
		i += 2
		if strings.HasPrefix(c.Status, "R") || strings.HasPrefix(c.Status, "C") {
			if i >= len(parts) {
				return nil, &Error{Op: "diff", Detail: "invalid rename output", Fix: r.fix("status")}
			}
			c.OldPath = c.Path
			c.Path = parts[i]
			i++
		}
		result = append(result, c)
	}
	return result, nil
}

// Commit stages only paths in this worktree. Empty paths means all changes.
// Existing staged changes are refused so an explicit paths list cannot leak them.
func (r *Repository) Commit(paths []string, message string) error {
	staged, e := r.git("diff", "--cached", "--name-only")
	if e != nil {
		return e
	}
	if staged != "" {
		return &Error{Op: "commit", Detail: "index already contains staged changes", Fix: r.fix("restore --staged .")}
	}
	for _, p := range paths {
		c := filepath.Clean(p)
		if filepath.IsAbs(p) || c == ".." || strings.HasPrefix(c, ".."+string(filepath.Separator)) {
			return &Error{Op: "commit", Detail: "path escapes worktree: " + p, Fix: r.fix("status")}
		}
	}
	if len(paths) == 0 {
		paths = []string{"."}
	}
	args := append([]string{"add", "--all", "--"}, paths...)
	if _, e = r.git(args...); e != nil {
		return e
	}
	changed, e := r.git("diff", "--cached", "--name-only")
	if e != nil {
		return e
	}
	if changed == "" {
		return nil
	}
	_, e = r.git("commit", "-m", message)
	return e
}
func (r *Repository) remoteRef() string { return "refs/remotes/" + r.Remote + "/" + r.Branch }
func (r *Repository) fetch() (bool, error) {
	out, e := r.git("ls-remote", "--heads", r.Remote, "refs/heads/"+r.Branch)
	if e != nil {
		return false, e
	}
	if out == "" {
		return false, nil
	}
	_, e = r.git("fetch", r.Remote, "+refs/heads/"+r.Branch+":"+r.remoteRef())
	return true, e
}
func (r *Repository) pull() error {
	exists, e := r.fetch()
	if e != nil || !exists {
		return e
	}
	head, e := r.Head()
	if e != nil {
		return e
	}
	if head == "" {
		_, e = r.git("reset", "--hard", r.remoteRef())
		return e
	}
	if _, e = r.git("rebase", r.remoteRef()); e != nil {
		out, _ := r.git("diff", "--name-only", "--diff-filter=U", "-z")
		files := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
		if _, abort := r.git("rebase", "--abort"); abort != nil {
			return abort
		}
		if out != "" {
			return &ConflictError{Files: files, Fix: r.fix("rebase " + r.remoteRef())}
		}
		return e
	}
	return nil
}
func (r *Repository) push() error {
	h, e := r.Head()
	if e != nil || h == "" {
		return e
	}
	_, e = r.git("push", r.Remote, "HEAD:refs/heads/"+r.Branch)
	return e
}

// Sync commits local edits then rebases and pushes only this branch.
func (r *Repository) Sync() error {
	if e := r.Commit(nil, "Sync "+r.Branch); e != nil {
		return e
	}
	if r.Remote == "" {
		return nil
	}
	if e := r.pull(); e != nil {
		return e
	}
	return r.push()
}

// Update implements push-as-lock. It requires a clean worktree so race recovery
// never discards unrelated edits. fn may read/write Dir but must not run Git.
// On callback failure its edits are preserved, without committing or pushing.
func (r *Repository) Update(fn func(*Repository) error) error {
	status, e := r.git("status", "--porcelain")
	if e != nil {
		return e
	}
	if status != "" {
		return &Error{Op: "update", Detail: "worktree has uncommitted changes", Fix: r.fix("add --all") + " && " + r.fix("commit -m "+strconv.Quote("Save local changes"))}
	}
	if r.Remote != "" {
		if e = r.pull(); e != nil {
			return e
		}
	}
	if e = fn(r); e != nil {
		return e
	}
	if e = r.Commit(nil, "Update "+r.Branch); e != nil {
		return e
	}
	if r.Remote == "" {
		return nil
	}
	if e = r.push(); e == nil {
		return nil
	}
	pushErr := e
	exists, e := r.fetch()
	if e != nil {
		return e
	}
	if !exists {
		return pushErr
	}
	// Distinguish a real non-fast-forward from authentication/hooks/network errors.
	if _, e = r.git("merge-base", "--is-ancestor", r.remoteRef(), "HEAD"); e == nil {
		return pushErr
	}
	if _, e = r.git("reset", "--hard", r.remoteRef()); e != nil {
		return e
	}
	h, e := r.Head()
	if e != nil {
		return e
	}
	return &ErrRaced{RemoteHead: h, Dir: r.Dir, Fix: r.fix("show HEAD")}
}
