package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/veridit/huginn/internal/cli"
)

type recorder struct{ args []string }

func (r *recorder) Execute(_ context.Context, args []string) (cli.Reply, error) {
	r.args = args
	if args[0] == "done" {
		return nil, &cli.UsageError{Problem: "blocked example", Fix: "huginn check abc123 1"}
	}
	return cli.Reply{"next": "huginn take abc123", "args": args}, nil
}
func TestToolsSchemasAndRoundTrip(t *testing.T) {
	ctx := context.Background()
	r := &recorder{}
	server := New(r)
	ct, st := sdk.NewInMemoryTransports()
	ss, e := server.Connect(ctx, st, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer ss.Close()
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil)
	cs, e := client.Connect(ctx, ct, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer cs.Close()
	tools, e := cs.ListTools(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	if len(tools.Tools) != 10 {
		t.Fatalf("tools=%d", len(tools.Tools))
	}
	for _, tool := range tools.Tools {
		if tool.Description == "" {
			t.Fatal(tool.Name)
		}
		b, _ := json.Marshal(tool.InputSchema)
		if strings.Contains(string(b), `"properties":{"properties"`) {
			t.Fatal(string(b))
		}
	}
	call := func(name string, input map[string]any) *sdk.CallToolResult {
		t.Helper()
		out, e := cs.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: input})
		if e != nil {
			t.Fatal(e)
		}
		if len(out.Content) == 0 {
			t.Fatal("missing content")
		}
		return out
	}
	out := call("new", map[string]any{"title": "A", "goal": "G", "accept": []string{"C", "D"}, "type": "bug"})
	if out.IsError {
		t.Fatal(out)
	}
	if !reflect.DeepEqual(r.args, []string{"new", "A", "--goal", "G", "--accept", "C", "--accept", "D", "--type", "bug"}) {
		t.Fatal(r.args)
	}
	call("update", map[string]any{"id": "abc123", "action": "check", "item": 2})
	if !reflect.DeepEqual(r.args, []string{"check", "abc123", "2"}) {
		t.Fatal(r.args)
	}
	call("next", map[string]any{"mine": true, "type": "bug"})
	if !reflect.DeepEqual(r.args, []string{"next", "--type", "bug", "--mine"}) {
		t.Fatal(r.args)
	}
	call("update", map[string]any{"id": "abc123", "action": "move_group", "group": "auth"})
	if !reflect.DeepEqual(r.args, []string{"mv", "abc123", "auth"}) {
		t.Fatal(r.args)
	}
	previous := append([]string(nil), r.args...)
	out = call("update", map[string]any{"id": "abc123", "action": "move", "group": "auth"})
	if !out.IsError || !reflect.DeepEqual(r.args, previous) {
		t.Fatal("folder movement must never accidentally rerank", r.args)
	}
	for _, tc := range []struct {
		action string
		fields map[string]any
		want   []string
	}{
		{"check", map[string]any{"item": 1}, []string{"check", "abc123", "1"}},
		{"note", map[string]any{"text": "Blocked by API"}, []string{"note", "abc123", "Blocked by API"}},
		{"move", map[string]any{"top": true}, []string{"move", "abc123", "--top"}},
		{"move_group", map[string]any{"group": "auth"}, []string{"mv", "abc123", "auth"}},
		{"drop", nil, []string{"drop", "abc123"}},
	} {
		input := map[string]any{"id": "abc123", "action": tc.action, "status": "doing", "type": "chore", "item": 1, "title": "", "text": "", "assignee": "", "label": "", "priority": "", "before": "", "after": "", "group": "", "top": false, "add_deps": []any{}, "remove_deps": nil}
		for key, value := range tc.fields {
			input[key] = value
		}
		out = call("update", input)
		if out.IsError || !reflect.DeepEqual(r.args, tc.want) {
			t.Fatalf("bridge action %s: error=%v args=%v", tc.action, out, r.args)
		}
	}
	previous = append([]string(nil), r.args...)
	out = call("update", map[string]any{"id": "abc123", "action": "move_group", "group": "auth", "top": true})
	if !out.IsError || !reflect.DeepEqual(r.args, previous) {
		t.Fatal("meaningful mixed movement must remain rejected", r.args)
	}
	out = call("update", map[string]any{"id": "abc123", "action": "check", "item": nil})
	if !out.IsError {
		t.Fatal("null cannot supply required action item")
	}
	call("update", map[string]any{"id": "abc123", "add_deps": []string{"def456"}, "remove_deps": []string{"old789"}})
	if !reflect.DeepEqual(r.args, []string{"dep", "abc123", "--add", "def456", "--remove", "old789"}) {
		t.Fatal(r.args)
	}
	out = call("done", map[string]any{"id": "abc123"})
	if !out.IsError {
		t.Fatal("error should be a tool error")
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), "huginn check abc123 1") {
		t.Fatal(string(b))
	}
	out = call("new", map[string]any{"title": "A", "goal": "G", "accept": []string{"C"}, "type": "typo"})
	if !out.IsError {
		t.Fatal("enum accepted")
	}
}
