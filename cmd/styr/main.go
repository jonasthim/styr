package main

import (
	"fmt"
	"io"
	"os"
)

var (
	version = "dev"
	commit  = "none"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout)) }

func run(args []string, stdout io.Writer) int {
	if len(args) == 0 {
		args = []string{"serve"}
	}
	switch args[0] {
	case "version":
		fmt.Fprintf(stdout, "styr %s (%s)\n", version, commit)
		return 0
	case "serve":
		return runServe(stdout)
	case "migrate":
		return runMigrate(stdout)
	case "doctor":
		return runDoctor(stdout)
	default:
		fmt.Fprintf(stdout, "unknown command %q\nusage: styr [serve|migrate|doctor|version]\n", args[0])
		return 2
	}
}
