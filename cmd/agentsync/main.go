// Command agentsync keeps AI coding-agent context files in sync
// (docs/design/tool-design.md). sync/--check are implemented; lint, context,
// and migration flags are still stubs the spec suite holds red.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"agentsync/internal/engine"
)

const usage = `usage: agentsync <sync|lint|context> [flags]`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "sync":
		fs := flag.NewFlagSet("sync", flag.ExitOnError)
		check := fs.Bool("check", false, "verify without writing; non-zero exit on unpropagated edits")
		fs.String("to", "", "migration target provider (not implemented)")
		fs.String("prune", "", "provider whose files to remove after migration (not implemented)")
		_ = fs.Parse(os.Args[2:])
		code, diags := engine.Sync(".", *check)
		writeReport(code, diags)
		os.Exit(code)
	case "lint", "context":
		writeReport(0, nil)
		os.Exit(0)
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
}

// writeReport emits the structured run report consumed by CI and the e2e
// runner (design §7) when AGENTSYNC_REPORT names a path.
func writeReport(exit int, diags []engine.Diag) {
	path := os.Getenv("AGENTSYNC_REPORT")
	if path == "" {
		return
	}
	if diags == nil {
		diags = []engine.Diag{}
	}
	data, err := json.MarshalIndent(struct {
		Exit        int           `json:"exit"`
		Diagnostics []engine.Diag `json:"diagnostics"`
	}{exit, diags}, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, append(data, '\n'), 0o644)
}
