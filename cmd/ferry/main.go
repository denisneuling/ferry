// Command ferry shows SMB shares in a browser: `ferry serve` will run the
// interface and the file API, logging people in through an SMB session setup
// against the configured server.
//
// It is configured through the environment; docs/configuration.md will list
// every variable. This is the repository's skeleton: only `version` and
// `help` work yet.
package main

import (
	"fmt"
	"io"
	"os"
)

// Set at build time through -ldflags (Makefile, Dockerfile, release.yml).
var (
	version = "dev"
	commit  = "unknown"
	builtAt = "unknown"
)

const usage = `ferry — SMB shares in the browser

Usage:
  ferry version   print the build
  ferry help      this text
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version", "--version":
		_, _ = fmt.Fprintf(stdout, "ferry %s (commit %s, built %s)\n", version, commit, builtAt)
		return 0
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(stdout, usage)
		return 0
	}
	_, _ = fmt.Fprintf(stderr, "ferry: unknown command %q\n\n%s", args[0], usage)
	return 2
}
