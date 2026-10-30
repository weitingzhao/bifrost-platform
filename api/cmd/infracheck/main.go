// infracheck prints the shared infra quick check or its Python setup for sh.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/infracheck"
)

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("infracheck", flag.ContinueOnError)
	flags.SetOutput(stderr)
	check := flags.Bool("print-check", false, "print the infra quick check script")
	setup := flags.Bool("print-setup", false, "print the Python environment setup script")
	wheelhouse := flags.String("wheelhouse", "", "install pinned wheels from this directory without an index")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *check == *setup || flags.NArg() != 0 || (*wheelhouse != "" && !*setup) {
		fmt.Fprintln(stderr, "use --print-check or --print-setup [--wheelhouse DIR]")
		return 2
	}
	script := infracheck.Check()
	if *setup {
		script = infracheck.SetupScript(*wheelhouse)
	}
	if _, err := io.WriteString(stdout, script); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
