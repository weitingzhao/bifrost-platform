// domainscore prints every domain's score against config/quality-ruler.json
// for the checkout it is run in:
//
//	go run ./cmd/domainscore -repo ..        (from api/)
//
// It reads the code with `go list`, the ownership table and the ruler, and
// writes JSON. It changes nothing in the checkout. For the contracts item it
// type-checks every package of the module (go/types over the export data
// `go list -export` names, which compiles the module into the build cache);
// the deployed platform never runs it. The reading is domainscore.Load.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/weitingzhao/bifrost-platform/api/internal/domainscore"
)

func run() error {
	repo := flag.String("repo", "..", "the repository root")
	flag.Parse()
	root, err := filepath.Abs(*repo)
	if err != nil {
		return err
	}
	in, _, err := domainscore.Load(root)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(domainscore.Score(in))
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "domainscore:", err)
		os.Exit(1)
	}
}
