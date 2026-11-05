// A tiny standalone acceptance driver, independent of the CLI lane.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/veridit/huginn/internal/task"
	"github.com/veridit/huginn/internal/tui"
)

func main() {
	root := os.Getenv("HUGINN_TUI_TEST_ROOT")
	if root == "" {
		fmt.Fprintln(os.Stderr, "HUGINN_TUI_TEST_ROOT is required")
		os.Exit(2)
	}
	s := &task.Store{Root: root}
	if len(os.Args) > 1 && os.Args[1] == "seed" {
		for _, o := range []task.NewOptions{
			{Title: "Alpha task", Goal: "Ship alpha safely", Acceptance: []string{"first acceptance", "second acceptance"}, Type: "bug", Priority: "high"},
			{Title: "Beta task", Goal: "Ship beta", Acceptance: []string{"beta acceptance"}, Type: "docs"},
		} {
			if _, e := s.New(o); e != nil {
				fmt.Fprintln(os.Stderr, e)
				os.Exit(1)
			}
		}
		return
	}
	if e := tui.Run(context.Background(), s, tui.Options{User: "pty-tester"}); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
