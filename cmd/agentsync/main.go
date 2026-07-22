// Command agentsync keeps AI coding-agent context files in sync
// (docs/design/tool-design.md). See Tasks.md for implementation roadmap.
package main

import (
	"fmt"
	"os"
)

const usage = `usage: agentsync <sync|lint|context> [flags]`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "sync", "lint", "context":
		fmt.Fprintln(os.Stderr, "not yet implemented; see Tasks.md")
		os.Exit(1)
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
}
