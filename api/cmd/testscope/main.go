// testscope prints a conservative, machine-readable Go test selection.
package main

import (
	"flag"
	"os"

	"github.com/weitingzhao/bifrost-platform/api/internal/testscope"
)

func main() {
	module := flag.String("module", ".", "Go module directory")
	stdin := flag.Bool("stdin", false, "read git name-status changes from stdin (history checks omitted)")
	flag.Parse()
	var r testscope.Result
	if *stdin {
		r = testscope.SelectChanges(*module, os.Stdin)
	} else if flag.NArg() != 2 {
		r = testscope.Full("expected base and head commits")
	} else {
		r = testscope.Select(*module, flag.Arg(0), flag.Arg(1))
	}
	r.Write(os.Stdout)
}
