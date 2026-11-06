// testscope prints a conservative, machine-readable Go or Console test selection.
package main

import (
	"flag"
	"os"
	"path/filepath"

	"github.com/weitingzhao/bifrost-platform/api/internal/testscope"
)

func main() {
	module := flag.String("module", ".", "Go module directory")
	stdin := flag.Bool("stdin", false, "read git name-status changes from stdin (history checks omitted)")
	console := flag.Bool("console", false, "select Console work (repository is parent of module)")
	flag.Parse()
	if *console {
		// Resolve before taking the parent: filepath.Dir(".") is still ".".
		abs, err := filepath.Abs(*module)
		if err != nil {
			testscope.ConsoleFull("module path error").Write(os.Stdout)
			return
		}
		root := filepath.Dir(abs)
		var r testscope.ConsoleResult
		if *stdin {
			r = testscope.SelectConsoleChanges(root, os.Stdin)
		} else if flag.NArg() != 2 {
			r = testscope.ConsoleFull("expected base and head commits")
		} else {
			r = testscope.SelectConsoleCommits(root, flag.Arg(0), flag.Arg(1))
		}
		r.Write(os.Stdout)
		return
	}
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
