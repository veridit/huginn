// Package mcp exposes the same task commands as the CLI over standard I/O.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/veridit/huginn/internal/task"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/veridit/huginn/internal/cli"
)

const Instructions = "Huginn keeps project tasks on a dedicated Git branch.\nUse list or next, then take, update acceptance, and done.\nEvery result includes a next step, and errors include exact fixes."

type Executor interface {
	Execute(context.Context, []string) (cli.Reply, error)
}

// New creates ten flat tools with the CLI's canonical JSON contract and errors.
func New(e Executor) *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: "huginn", Version: "0.1.0"}, &sdk.ServerOptions{Instructions: Instructions})
	var mu sync.Mutex
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	enum := func(values ...string) map[string]any { return map[string]any{"type": "string", "enum": values} }
	filters := func() map[string]any {
		return map[string]any{"type": enum("feature", "bug", "chore", "spike", "docs"), "priority": str("Priority label"), "label": str("Label filter"), "group": str("Exact folder"), "mine": map[string]any{"type": "boolean"}}
	}
	add := func(name, desc string, props map[string]any, required []string, build func(map[string]any) ([]string, error)) {
		schema := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
		if len(required) > 0 {
			schema["required"] = required
		}
		// Raw registration lets even schema errors carry the same actionable next hint.
		s.AddTool(&sdk.Tool{Name: name, Description: desc, InputSchema: schema}, func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			mu.Lock()
			defer mu.Unlock()
			input := map[string]any{}
			var err error
			if len(req.Params.Arguments) > 0 {
				if e := json.Unmarshal(req.Params.Arguments, &input); e != nil {
					err = &cli.UsageError{Problem: "invalid tool arguments: " + e.Error(), Fix: "huginn help"}
				}
			}
			if err == nil {
				err = validate(input, props, required)
			}
			var args []string
			if err == nil {
				args, err = build(input)
			}
			var reply cli.Reply
			if err == nil {
				reply, err = e.Execute(ctx, args)
			}
			failed := err != nil
			if failed {
				code, p, fix := cli.ErrorDetails(err)
				reply = cli.Reply{"error": p, "fix": fix, "next": fix, "exit_code": code}
			}
			b, err := json.Marshal(reply)
			if err != nil {
				return nil, err
			}
			return &sdk.CallToolResult{IsError: failed, Content: []sdk.Content{&sdk.TextContent{Text: string(b)}}, StructuredContent: reply}, nil
		})
	}
	get := func(m map[string]any, k string) string { v, _ := m[k].(string); return v }
	flags := func(args []string, m map[string]any, keys ...string) []string {
		for _, k := range keys {
			if v, ok := m[k]; ok {
				switch v := v.(type) {
				case string:
					args = append(args, "--"+k, v)
				case bool:
					if v {
						args = append(args, "--"+k)
					}
				case []any:
					for _, x := range v {
						args = append(args, "--"+k, fmt.Sprint(x))
					}
				}
			}
		}
		return args
	}
	p := filters()
	p["status"] = enum("todo", "doing", "done", "dropped")
	add("list", "List tasks in rank order, including done and dropped unless filtered.", p, nil, func(m map[string]any) ([]string, error) {
		return flags([]string{"ls"}, m, "type", "priority", "label", "group", "mine", "status"), nil
	})
	add("show", "Show a task by id, unique prefix, slug or alias with dependencies and acceptance.", map[string]any{"id": str("Task reference")}, []string{"id"}, func(m map[string]any) ([]string, error) { return []string{"show", get(m, "id")}, nil })
	array := func(desc string) map[string]any {
		return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
	}
	add("new", "Create a task with a goal, acceptance checks and optional dependency references.", map[string]any{"title": str("Task title"), "goal": str("Desired outcome"), "accept": array("Observable acceptance checks"), "dep": array("Dependency ids, slugs or aliases"), "type": enum("feature", "bug", "chore", "spike", "docs"), "priority": str("Priority label"), "label": array("Labels"), "group": str("Folder"), "top": map[string]any{"type": "boolean"}, "after": str("Task to follow")}, []string{"title", "goal", "accept"}, func(m map[string]any) ([]string, error) {
		return flags([]string{"new", get(m, "title")}, m, "goal", "accept", "dep", "type", "priority", "label", "group", "top", "after"), nil
	})
	add("update", "Change fields (default action set), dependencies (dep), checks, notes, rank, folder (move_group), or drop a task.", map[string]any{"id": str("Task reference"), "action": enum("set", "check", "note", "move", "move_group", "mv", "dep", "drop", "rebalance"), "status": enum("todo", "doing", "done", "dropped"), "type": enum("feature", "bug", "chore", "spike", "docs"), "priority": str("Priority label"), "title": str("New title"), "label": str("Label"), "assignee": str("Assignee"), "item": map[string]any{"type": "integer", "minimum": 1}, "text": str("Note text"), "add_deps": array("Dependency references to add, idempotently; use action dep"), "remove_deps": array("Dependency references to remove; use action dep"), "group": str("Destination folder, requires action move_group and preserves rank and contents"), "before": str("Insert before task"), "after": str("Insert after task"), "top": map[string]any{"type": "boolean"}}, []string{"id"}, func(m map[string]any) ([]string, error) {
		action, id := get(m, "action"), get(m, "id")
		if action == "" {
			action = "set"
			if _, ok := m["add_deps"]; ok {
				action = "dep"
			}
			if _, ok := m["remove_deps"]; ok {
				action = "dep"
			}
		}
		allowed := map[string]string{"set": "status type priority title label assignee", "check": "item", "note": "text", "move": "before after top", "move_group": "group", "mv": "group", "dep": "add_deps remove_deps", "drop": "", "rebalance": ""}
		permitted := strings.Fields(allowed[action] + " id action")
		for key := range m {
			if !slices.Contains(permitted, key) {
				fix := "huginn help"
				if key == "group" {
					fix = "huginn mv " + id + " " + get(m, "group")
				}
				return nil, &cli.UsageError{Problem: "argument " + key + " does not apply to action " + action + "; use move_group for folders or dep for dependencies", Fix: fix}
			}
		}
		args := []string{action, id}
		switch action {
		case "set":
			args = flags(args, m, "status", "type", "priority", "title", "label", "assignee")
		case "check":
			n, ok := m["item"].(float64)
			if !ok {
				return nil, &cli.UsageError{Problem: "check needs item", Fix: "huginn show " + id}
			}
			args = append(args, strconv.Itoa(int(n)))
		case "note":
			args = append(args, get(m, "text"))
		case "mv", "move_group":
			if _, ok := m["group"]; !ok {
				return nil, &cli.UsageError{Problem: "move_group needs group", Fix: "huginn show " + id}
			}
			args = []string{"mv", id, get(m, "group")}
		case "dep":
			args = []string{"dep", id}
			for _, pair := range [][2]string{{"add_deps", "add"}, {"remove_deps", "remove"}} {
				if values, ok := m[pair[0]].([]any); ok {
					for _, v := range values {
						args = append(args, "--"+pair[1], v.(string))
					}
				}
			}
		case "move":
			if _, ok := m["group"]; ok {
				return nil, &cli.UsageError{Problem: "action move reorders rank; to change folder use action move_group with group", Fix: "huginn mv " + id + " " + get(m, "group")}
			}
			args = flags(args, m, "before", "after", "top")
		}
		return args, nil
	})
	for _, cmd := range []string{"take", "done"} {
		add(cmd, ""+map[string]string{"take": "Claim a task using the remote push lock and set it doing.", "done": "Finish a task after dependencies and acceptance checks are complete."}[cmd], map[string]any{"id": str("Task reference")}, []string{"id"}, func(m map[string]any) ([]string, error) { return []string{cmd, get(m, "id")}, nil })
	}
	add("next", "Find the highest-ranked available task or your doing task with combined filters.", filters(), nil, func(m map[string]any) ([]string, error) {
		return flags([]string{"next"}, m, "type", "priority", "label", "group", "mine"), nil
	})
	for _, cmd := range []string{"lint", "sync", "types"} {
		add(cmd, map[string]string{"lint": "Report malformed files, dangling dependencies, cycles and unchecked done tasks.", "sync": "Commit local edits, rebase and push the task branch only.", "types": "List the five supported task types and their meanings."}[cmd], map[string]any{}, nil, func(map[string]any) ([]string, error) { return []string{cmd}, nil })
	}
	return s
}
func validate(input map[string]any, props map[string]any, required []string) error {
	bad := func(message string) error { return &cli.UsageError{Problem: message, Fix: "huginn help"} }
	for _, key := range required {
		if _, ok := input[key]; !ok {
			return bad("missing argument " + key)
		}
	}
	keys := []string{}
	for key := range props {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for key, value := range input {
		raw, ok := props[key]
		if !ok {
			e := task.Invalid("argument", key, keys)
			return bad("unknown argument " + key + "; valid: " + strings.Join(keys, ", ") + "; closest: " + e.Closest)
		}
		spec := raw.(map[string]any)
		valid := false
		switch spec["type"] {
		case "string":
			_, valid = value.(string)
		case "boolean":
			_, valid = value.(bool)
		case "integer":
			v, ok := value.(float64)
			valid = ok && math.Trunc(v) == v && v >= 1
		case "array":
			items, ok := value.([]any)
			valid = ok
			for _, v := range items {
				if _, ok := v.(string); !ok {
					valid = false
				}
			}
		}
		if !valid {
			return bad("invalid " + key + ": expected " + fmt.Sprint(spec["type"]))
		}
		if options, ok := spec["enum"].([]string); ok && !slices.Contains(options, value.(string)) {
			e := task.Invalid(key, value.(string), options)
			e.Fix = "huginn help"
			return e
		}
	}
	return nil
}

func Run(ctx context.Context, e Executor) error { return New(e).Run(ctx, &sdk.StdioTransport{}) }
