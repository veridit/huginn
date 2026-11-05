// Package cli is the shared non-interactive command surface for CLI and MCP.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/veridit/huginn/gitbranch"
	"github.com/veridit/huginn/internal/cache"
	"github.com/veridit/huginn/internal/task"
)

const Help = `huginn: tasks on a Git branch, for humans and agents.
Use --json with any command. IDs, unique prefixes, slugs and aliases work.
  init                      huginn init
  new                       huginn new "Fix retry" --type bug --goal "Retries work" --accept "Test passes"
  ls                        huginn ls --status todo --label api --mine
  show                      huginn show k7m2qx
  edit                      huginn edit k7m2qx
  move                      huginn move k7m2qx --before p3rt9a
  mv                        huginn mv k7m2qx auth
  take                      huginn take k7m2qx
  done                      huginn done k7m2qx
  check                     huginn check k7m2qx 1
  note                      huginn note k7m2qx "Test evidence"
  dep                       huginn dep k7m2qx --add p3rt9a
  set                       huginn set k7m2qx --priority high --label api
  drop                      huginn drop k7m2qx
  next                      huginn next --type bug --priority high --group auth
  rebalance                 huginn rebalance
  types                     huginn types
  lint                      huginn lint
  sync                      huginn sync
  import                    huginn import backlog ./backlog
  tui                       huginn tui
  mcp                       huginn mcp
  help                      huginn help
New flags: --dep ID (repeatable), --group FOLDER, --label LABEL, --top, --after ID.
Details: huginn help COMMAND (for every flat flag and examples).
Identity: HUGINN_USER, otherwise git config user.name.
Exit: 0 success, 1 error, 2 usage, 3 blocked, 4 no next task.
next: huginn next
`

var Commands = []string{"init", "new", "ls", "show", "edit", "move", "mv", "take", "done", "check", "note", "dep", "set", "drop", "next", "rebalance", "types", "lint", "sync", "import", "tui", "mcp", "help"}

type UsageError struct{ Problem, Fix string }
type OperationError struct{ Problem, Fix string }

func (e *OperationError) Error() string { return e.Problem }

func (e *UsageError) Error() string   { return e.Problem }
func usage(problem, fix string) error { return &UsageError{problem, fix} }

type App struct {
	Cwd      string
	In       io.Reader
	Out, Err io.Writer
}
type Reply map[string]any

type gitCache struct{ r *gitbranch.Repository }

func (g gitCache) Head(context.Context) (string, error) { return g.r.Head() }
func (g gitCache) Diff(_ context.Context, a, b string) ([]cache.Change, error) {
	cs, e := g.r.DiffNameStatus(a, b)
	out := make([]cache.Change, 0, len(cs))
	for _, c := range cs {
		out = append(out, cache.Change{Status: c.Status, OldPath: c.OldPath, Path: c.Path})
	}
	return out, e
}

type session struct {
	r *gitbranch.Repository
	c *cache.Cache
	s *task.Store
}

func (a *App) open(ctx context.Context) (*session, error) {
	cwd := a.Cwd
	if cwd == "" {
		cwd = "."
	}
	r, e := gitbranch.Open(cwd, "huginn", ".huginn")
	if e != nil {
		return nil, e
	}
	cmd := exec.CommandContext(ctx, "git", "-C", r.Dir, "rev-parse", "--git-common-dir")
	b, e := cmd.Output()
	if e != nil {
		return nil, e
	}
	gd := strings.TrimSpace(string(b))
	if !filepath.IsAbs(gd) {
		gd = filepath.Join(r.Dir, gd)
	}
	c, e := cache.Open(r.Dir, filepath.Join(gd, "huginn", "cache.db"), gitCache{r})
	if e != nil {
		return nil, e
	}
	s := &task.Store{Root: r.Dir}
	s.Refresh = func() ([]task.Task, error) {
		if _, e := c.Sync(ctx); e != nil {
			return nil, e
		}
		return c.List(ctx)
	}
	if _, e = s.Tasks(); e != nil {
		c.Close()
		return nil, e
	}
	return &session{r, c, s}, nil
}
func (a *App) refreshExisting(ctx context.Context) error {
	cwd := a.Cwd
	if cwd == "" {
		cwd = "."
	}
	c := exec.CommandContext(ctx, "git", "-C", cwd, "rev-parse", "--show-toplevel")
	b, e := c.Output()
	if e != nil {
		return nil
	}
	if _, e = os.Lstat(filepath.Join(strings.TrimSpace(string(b)), ".huginn")); os.IsNotExist(e) {
		return nil
	} else if e != nil {
		return e
	}
	s, e := a.open(ctx)
	if e != nil {
		return e
	}
	return s.c.Close()
}

func (a *App) identity(ctx context.Context) (string, error) {
	if u := strings.TrimSpace(os.Getenv("HUGINN_USER")); u != "" {
		return u, nil
	}
	cmd := exec.CommandContext(ctx, "git", "config", "user.name")
	cmd.Dir = a.Cwd
	b, e := cmd.Output()
	if e == nil && strings.TrimSpace(string(b)) != "" {
		return strings.TrimSpace(string(b)), nil
	}
	return "", usage("identity missing: set HUGINN_USER or git config user.name", `export HUGINN_USER="your-name"`)
}

type flags struct {
	values map[string][]string
	args   []string
}

func parse(args []string, allowed string) (flags, error) {
	f := flags{values: map[string][]string{}}
	valid := strings.Fields(allowed + " json help")
	for i := 0; i < len(args); i++ {
		v := args[i]
		if v == "--" {
			f.args = append(f.args, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(v, "--") {
			f.args = append(f.args, v)
			continue
		}
		key, val, eq := strings.Cut(strings.TrimPrefix(v, "--"), "=")
		if !slices.Contains(valid, key) {
			e := task.Invalid("flag", key, valid)
			return f, usage("unknown flag --"+key+"; valid: --"+strings.Join(valid, ", --")+"; closest: --"+e.Closest, "huginn help")
		}
		if slices.Contains([]string{"json", "help", "mine", "top"}, key) {
			if eq && val != "true" {
				return f, usage("--"+key+" is a boolean flag", "huginn help")
			}
			f.values[key] = []string{"true"}
			continue
		}
		if !eq {
			i++
			if i >= len(args) || strings.HasPrefix(args[i], "--") {
				return f, usage("missing value for --"+key, "huginn help")
			}
			val = args[i]
		}
		f.values[key] = append(f.values[key], val)
	}
	return f, nil
}
func (f flags) get(k string) string {
	v := f.values[k]
	if len(v) > 0 {
		return v[len(v)-1]
	}
	return ""
}
func (f flags) has(k string) bool { _, ok := f.values[k]; return ok }
func (f flags) count(n int) error {
	if len(f.args) != n {
		return usage(fmt.Sprintf("expected %d positional arguments, got %d", n, len(f.args)), "huginn help")
	}
	return nil
}

func taskJSON(t task.Task, root string, ts []task.Task) Reply {
	b, _ := json.Marshal(t)
	out := Reply{}
	_ = json.Unmarshal(b, &out)
	out["goal"] = strings.TrimSpace(t.Goal)
	acceptance := []any{}
	for _, item := range t.Acceptance {
		acceptance = append(acceptance, map[string]any{"text": item.Text, "checked": item.Checked})
	}
	out["acceptance"] = acceptance
	deps := []string{}
	for _, d := range t.Dependencies {
		v, e := task.ResolveDependency(ts, d.Ref)
		if e == nil {
			deps = append(deps, v.ID)
		} else {
			deps = append(deps, d.Ref)
		}
	}
	out["dependencies"] = deps
	out["dependency_details"] = task.DependencyDetails(t, ts)
	out["notes"] = ""
	if b, e := os.ReadFile(filepath.Join(root, filepath.FromSlash(t.Path))); e == nil {
		lines := strings.Split(string(b), "\n")
		active := false
		notes := []string{}
		for _, l := range lines {
			if strings.HasPrefix(l, "## ") {
				if active {
					break
				}
				active = strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(l, "## ")), "Notes")
				continue
			}
			if active {
				notes = append(notes, l)
			}
		}
		out["notes"] = strings.TrimSpace(strings.Join(notes, "\n"))
	}
	for _, k := range []string{"assignee", "labels", "aliases", "acceptance", "dependency_details"} {
		if out[k] == nil {
			out[k] = []any{}
		}
	}
	return out
}
func resultJSON(r task.Result, s *session) (Reply, error) {
	if r.Task.ID == "" {
		return Reply{"changed": r.Changed, "next": r.Next, "renamed": r.Renamed}, nil
	}
	ts, e := s.s.Tasks()
	if e != nil {
		return nil, e
	}
	out := Reply{"task": taskJSON(r.Task, s.r.Dir, ts), "changed": r.Changed, "next": r.Next}
	if len(r.Warnings) > 0 {
		out["warnings"] = r.Warnings
	}
	if len(r.Renamed) > 0 {
		out["renamed"] = r.Renamed
		out["renames"] = r.Renames
	}
	return out, nil
}

// Execute performs exactly the same operation for shell and MCP callers.
func (a *App) Execute(ctx context.Context, args []string) (Reply, error) {
	cmd := "help"
	if len(args) > 0 {
		cmd = args[0]
		args = args[1:]
		if cmd == "--help" || cmd == "--json" {
			args = append([]string{cmd}, args...)
			cmd = "help"
		}
	}
	if !slices.Contains(Commands, cmd) {
		e := task.Invalid("command", cmd, Commands)
		return nil, usage("unknown command "+cmd+"; valid: "+strings.Join(Commands, ", ")+"; closest: "+e.Closest, "huginn "+e.Closest+" --help")
	}
	allowed := map[string]string{"new": "type priority goal accept dep group label top after", "ls": "status type priority label group mine", "next": "type priority label group mine", "move": "before after top", "dep": "add remove", "set": "priority type label status title assignee"}
	f, e := parse(args, allowed[cmd])
	if e != nil {
		return nil, e
	}
	if f.has("help") || cmd == "help" {
		if e = a.refreshExisting(ctx); e != nil {
			return nil, e
		}
		target := cmd
		if cmd == "help" {
			if len(f.args) > 1 {
				return nil, usage("help expects at most one command", "huginn help")
			}
			if len(f.args) == 0 {
				return Reply{"help": Help, "next": "huginn next"}, nil
			}
			target = f.args[0]
		}
		if !slices.Contains(Commands, target) {
			e := task.Invalid("command", target, Commands)
			return nil, usage("unknown command "+target+"; valid: "+strings.Join(Commands, ", ")+"; closest: "+e.Closest, "huginn help "+e.Closest)
		}
		return Reply{"help": commandHelp(target), "next": "huginn next"}, nil
	}
	if cmd == "types" {
		if e = a.refreshExisting(ctx); e != nil {
			return nil, e
		}
		if e = f.count(0); e != nil {
			return nil, e
		}
		return Reply{"types": task.TypeDescriptions, "next": "huginn next"}, nil
	}
	s, e := a.open(ctx)
	if e != nil {
		return nil, e
	}
	defer s.c.Close()
	empty := func() (Reply, error) {
		ts, e := s.s.Tasks()
		if e != nil {
			return nil, e
		}
		next := "huginn new \"title\" --goal \"outcome\" --accept \"observable check\""
		if r, e := task.Next(ts, task.Filter{}); e == nil {
			next = r.Next
		}
		return Reply{"next": next}, nil
	}
	filter := task.Filter{Type: f.get("type"), Priority: f.get("priority"), Label: f.get("label"), Group: f.get("group"), Status: f.get("status")}
	if f.has("mine") {
		filter.Mine, e = a.identity(ctx)
		if e != nil {
			return nil, e
		}
	}
	if filter.Type != "" && !slices.Contains(task.Types, filter.Type) {
		e := task.Invalid("type", filter.Type, task.Types)
		e.Fix = "huginn " + cmd + " --type " + e.Closest
		if cmd == "set" && len(f.args) == 1 {
			if d, err := s.s.Get(f.args[0]); err == nil {
				e.ID = d.Task.ID
				e.Fix = "huginn set " + d.Task.ID + " --type " + e.Closest
			}
		}
		if cmd == "new" {
			e.Fix = `huginn new "title" --goal "outcome" --accept "observable check" --type ` + e.Closest
		}
		return nil, e
	}
	if filter.Status != "" && !slices.Contains(task.Statuses, filter.Status) {
		e := task.Invalid("status", filter.Status, task.Statuses)
		e.Fix = "huginn ls --status " + e.Closest
		if cmd == "set" && len(f.args) == 1 {
			if d, err := s.s.Get(f.args[0]); err == nil {
				e.ID = d.Task.ID
				e.Fix = "huginn set " + d.Task.ID + " --status " + e.Closest
			}
		}
		return nil, e
	}
	var r task.Result
	switch cmd {
	case "init":
		if e = f.count(0); e != nil {
			return nil, e
		}
		e = s.r.Commit(nil, "huginn init")
		if e != nil {
			return nil, e
		}
		return empty()
	case "ls":
		if e = f.count(0); e != nil {
			return nil, e
		}
		ts, e := s.s.Tasks()
		if e != nil {
			return nil, e
		}
		out := []Reply{}
		for _, t := range ts {
			if filter.Status != "" && t.Status != filter.Status || filter.Type != "" && t.Type != filter.Type || filter.Priority != "" && t.Priority != filter.Priority || filter.Group != "" && t.Group != filter.Group || filter.Label != "" && !slices.Contains(t.Labels, filter.Label) || filter.Mine != "" && !slices.Contains(t.Assignee, filter.Mine) {
				continue
			}
			out = append(out, taskJSON(t, s.r.Dir, ts))
		}
		reply, e := empty()
		if len(out) > 0 {
			reply["next"] = "huginn show " + out[0]["id"].(string)
		}
		reply["tasks"] = out
		return reply, e
	case "show":
		if e = f.count(1); e != nil {
			return nil, e
		}
		d, e := s.s.Get(f.args[0])
		if e != nil {
			return nil, e
		}
		ts, e := s.s.Tasks()
		return Reply{"task": taskJSON(d.Task, s.r.Dir, ts), "next": task.NextHint(d.Task)}, e
	case "next":
		if e = f.count(0); e != nil {
			return nil, e
		}
		r, e = s.s.Next(filter)
	case "lint":
		if e = f.count(0); e != nil {
			return nil, e
		}
		fs, e := s.c.Lint(ctx)
		if e != nil {
			return nil, e
		}
		if fs == nil {
			fs = []task.Finding{}
		}
		reply, e := empty()
		reply["findings"] = fs
		return reply, e
	case "sync":
		if e = f.count(0); e != nil {
			return nil, e
		}
		if e = s.r.Sync(); e != nil {
			return nil, e
		}
		return empty()
	case "new":
		if e = f.count(1); e != nil {
			return nil, e
		}
		r, e = s.s.New(task.NewOptions{Title: f.args[0], Goal: f.get("goal"), Type: f.get("type"), Priority: f.get("priority"), Group: f.get("group"), Acceptance: f.values["accept"], Deps: f.values["dep"], Labels: f.values["label"], Top: f.has("top"), After: f.get("after")})
	case "take":
		if e = f.count(1); e != nil {
			return nil, e
		}
		r, e = a.take(ctx, s, f.args[0])
	case "dep":
		if e = f.count(1); e != nil {
			return nil, e
		}
		if !f.has("add") && !f.has("remove") {
			return nil, usage("dep needs --add REF or --remove REF", "huginn dep "+f.args[0]+" --help")
		}
		r, e = changeDependencies(s.s, f.args[0], f.values["add"], f.values["remove"])
	case "done", "drop":
		if e = f.count(1); e != nil {
			return nil, e
		}
		if cmd == "done" {
			r, e = s.s.Done(f.args[0])
		} else {
			r, e = s.s.Drop(f.args[0])
		}
	case "check":
		if e = f.count(2); e != nil {
			return nil, e
		}
		n, err := strconv.Atoi(f.args[1])
		if err != nil || n < 1 {
			return nil, usage("acceptance index must be a positive integer", "huginn show "+f.args[0])
		}
		r, e = s.s.Check(f.args[0], n)
	case "note":
		if e = f.count(2); e != nil {
			return nil, e
		}
		r, e = s.s.Note(f.args[0], f.args[1])
	case "set":
		if e = f.count(1); e != nil {
			return nil, e
		}
		fields := []string{"status", "type", "priority", "label", "title", "assignee"}
		count := 0
		for _, field := range fields {
			if f.has(field) {
				count++
			}
		}
		if count == 0 {
			return nil, usage("set needs a field: --priority, --type, --label, --status, --title, --assignee", "huginn show "+f.args[0])
		}
		if f.has("title") && strings.TrimSpace(f.get("title")) == "" {
			return nil, usage("title must not be empty", "huginn show "+f.args[0])
		}
		if v := f.get("status"); v != "" && !slices.Contains(task.Statuses, v) {
			return nil, task.Invalid("status", v, task.Statuses)
		}
		for _, field := range fields {
			if f.has(field) {
				r, e = s.s.Set(f.args[0], field, f.get(field))
				if e != nil {
					break
				}
			}
		}
	case "move":
		if e = f.count(1); e != nil {
			return nil, e
		}
		if !f.has("before") && !f.has("after") && !f.has("top") {
			return nil, usage("move needs --before ID, --after ID or --top; use mv for a group change", "huginn move "+f.args[0]+" --help")
		}
		r, e = s.s.Move(f.args[0], task.MoveOptions{Before: f.get("before"), After: f.get("after"), Top: f.has("top")})
	case "mv":
		if e = f.count(2); e != nil {
			return nil, e
		}
		r, e = s.s.MoveGroup(f.args[0], f.args[1])
	case "rebalance":
		r, e = s.s.Rebalance(f.args)
	case "edit":
		if e = f.count(1); e != nil {
			return nil, e
		}
		d, err := s.s.Get(f.args[0])
		if err != nil {
			return nil, err
		}
		editor := strings.Fields(os.Getenv("EDITOR"))
		if len(editor) == 0 {
			return nil, usage("EDITOR is not configured", "export EDITOR=vi")
		}
		c := exec.CommandContext(ctx, editor[0], append(editor[1:], filepath.Join(s.r.Dir, d.Task.Path))...)
		c.Stdin = a.In
		c.Stdout = a.Out
		if f.has("json") {
			c.Stdout = a.Err
		}
		c.Stderr = a.Err
		if e = c.Run(); e != nil {
			return nil, e
		}
		if e = s.r.Commit(nil, "huginn edit "+d.Task.ID); e != nil {
			return nil, e
		}
		fs, err := s.c.Lint(ctx)
		if err != nil {
			return nil, err
		}
		reply, err := empty()
		if fs == nil {
			fs = []task.Finding{}
		}
		reply["findings"] = fs
		reply["next"] = "huginn show " + d.Task.ID
		return reply, err
	case "import":
		return a.runImport(ctx, s, f)
	case "tui":
		return a.runTUI(ctx, s, f)
	default:
		return nil, usage("command requires its dedicated transport", "huginn help")
	}
	if e != nil {
		var detail *task.Error
		if errors.As(e, &detail) && strings.Contains(detail.Fix, "<id>") {
			if cmd == "new" {
				detail.Fix = "huginn help new"
			} else if len(f.args) > 0 {
				if d, err := s.s.Get(f.args[0]); err == nil {
					detail.ID = d.Task.ID
					detail.Fix = strings.ReplaceAll(detail.Fix, "<id>", d.Task.ID)
				}
			}
		}
		return nil, e
	}
	if cmd != "next" && cmd != "take" {
		if e = s.r.Commit(nil, "huginn "+cmd+" "+r.Task.ID); e != nil {
			return nil, e
		}
	}
	return resultJSON(r, s)
}

func ErrorDetails(err error) (int, string, string) {
	code, problem, fix := 1, strings.ReplaceAll(err.Error(), "\n", "; "), "huginn help"
	var op *OperationError
	if errors.As(err, &op) {
		return 1, op.Problem, op.Fix
	}
	var u *UsageError
	if errors.As(err, &u) {
		return 2, u.Problem, u.Fix
	}
	var t *task.Error
	if errors.As(err, &t) {
		code = t.ExitCode()
		if t.Kind == "taken" {
			return 1, "taken by " + t.Value + " (" + t.ID + ")", "huginn next"
		}
		fix = t.Fix
		problem = t.Kind
		if t.ID != "" {
			problem += " " + t.ID
		}
		if t.Field != "" {
			problem += " " + t.Field
		}
		if t.Value != "" {
			problem += ": " + t.Value
		}
		if len(t.Valid) > 0 {
			problem += "; valid: " + strings.Join(t.Valid, ", ") + "; closest: " + t.Closest
		}
		for _, m := range t.Matches {
			problem += "; " + m.Slug + " " + m.ID
		}
		fixes := []string{}
		for _, d := range t.Dependencies {
			problem += "; " + d.ID + " " + d.Title
			fixes = append(fixes, "huginn show "+d.ID)
		}
		for _, v := range t.Unchecked {
			problem += fmt.Sprintf("; %d. %s", v.Index, v.Text)
			fixes = append(fixes, v.Fix)
		}
		if len(fixes) > 0 && len(t.Cycle) == 0 {
			fix = strings.Join(fixes, " && ")
		}
	}
	var g *gitbranch.Error
	if errors.As(err, &g) {
		fix = g.Fix
	}
	var c *gitbranch.ConflictError
	if errors.As(err, &c) {
		fix = c.Fix
	}
	if fix == "" {
		fix = "huginn help"
	}
	problem = strings.ReplaceAll(strings.SplitN(problem, "; fix:", 2)[0], "\r", " ")
	return code, strings.ReplaceAll(problem, "\n", "; "), fix
}

func (a *App) Run(ctx context.Context, args []string) int {
	reply, e := a.Execute(ctx, args)
	jsonMode := isJSON(args)
	if e != nil {
		code, p, fix := ErrorDetails(e)
		p = strings.NewReplacer("\r", " ", "\n", "; ").Replace(p)
		if jsonMode {
			_ = json.NewEncoder(a.Out).Encode(Reply{"error": p, "fix": fix, "next": fix, "exit_code": code})
		} else {
			fmt.Fprintln(a.Err, p)
			fmt.Fprintln(a.Err, "fix: "+fix)
		}
		return code
	}
	if jsonMode {
		_ = json.NewEncoder(a.Out).Encode(reply)
		return 0
	}
	if h, ok := reply["help"].(string); ok {
		fmt.Fprint(a.Out, h)
		return 0
	}
	if ts, ok := reply["tasks"].([]Reply); ok {
		for _, t := range ts {
			fmt.Fprintf(a.Out, "%s  %s  %s  %s\n", t["slug"], t["title"], t["id"], t["status"])
		}
	}
	if t, ok := reply["task"].(Reply); ok {
		fmt.Fprintf(a.Out, "%s  %s  %s  %s\n", t["slug"], t["title"], t["id"], t["status"])
		fmt.Fprintf(a.Out, "goal: %s\n", t["goal"])
		for i, v := range t["acceptance"].([]any) {
			m := v.(map[string]any)
			mark := " "
			if m["checked"] == true {
				mark = "x"
			}
			fmt.Fprintf(a.Out, "%d. [%s] %s\n", i+1, mark, m["text"])
		}
		if t["notes"] != "" {
			fmt.Fprintf(a.Out, "notes: %s\n", t["notes"])
		}
		if ds, ok := t["dependency_details"].([]task.DependencyDetail); ok {
			for _, d := range ds {
				fmt.Fprintf(a.Out, "dependency: %s  %s  [%s]  %s\n", d.ID, d.Title, d.Status, d.Path)
			}
		}
	}
	if fs, ok := reply["findings"].([]task.Finding); ok {
		for _, v := range fs {
			fmt.Fprintln(a.Out, v.String())
		}
	}
	if ts, ok := reply["types"].([]task.TypeInfo); ok {
		for _, v := range ts {
			fmt.Fprintf(a.Out, "%s  %s\n", v.Value, v.Description)
		}
	}
	if ws, ok := reply["warnings"].([]string); ok {
		for _, v := range ws {
			fmt.Fprintln(a.Out, "warning: "+v)
		}
	}
	if ids, ok := reply["renamed"].([]string); ok {
		fmt.Fprintln(a.Out, "renamed: "+strings.Join(ids, ", "))
	}
	fmt.Fprintln(a.Out, "next: "+reply["next"].(string))
	return 0
}
