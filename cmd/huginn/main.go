package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"slices"

	"github.com/veridit/huginn/internal/cli"
	"github.com/veridit/huginn/internal/mcp"
)

func main() { os.Exit(run()) }
func run() int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	cwd, e := os.Getwd()
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		return 1
	}
	a := &cli.App{Cwd: cwd, In: os.Stdin, Out: os.Stdout, Err: os.Stderr}
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "mcp" && !slices.Contains(args, "--help") {
		if len(args) > 2 || (len(args) == 2 && args[1] != "--json") {
			return a.Run(ctx, []string{"mcp", "--invalid"})
		}
		if e = mcp.Run(ctx, a); e != nil {
			_, p, fix := cli.ErrorDetails(e)
			fmt.Fprintln(os.Stderr, p)
			fmt.Fprintln(os.Stderr, "fix: "+fix)
			return 1
		}
		return 0
	}
	return a.Run(ctx, args)
}
