// Command agentsync is a walking skeleton: it parses the CLI surface defined
// in docs/design/tool-design.md but implements no behavior yet. The e2e spec
// suite (test/e2e) is expected to fail against this stub — that is the TDD
// red state the implementation will turn green.
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
		fmt.Fprintf(os.Stderr, "agentsync %s: not implemented (walking skeleton)\n", os.Args[1])
		os.Exit(0)
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
}
