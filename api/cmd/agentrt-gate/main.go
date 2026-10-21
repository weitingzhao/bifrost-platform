// Command agentrt-gate runs a gate of the architecture table (TD-397,
// CUT-td397-gate-runs-2026-10-20.md), and prints the catalog's plan change.
//
//	agentrt-gate -pkg ./internal/agentrt/plan/ -test TestA [-test TestB ...]
//	agentrt-gate catalog -env prod|stg
//
// The first form is what a gate's spec runs on the commit an environment
// runs (from api/): go test -json on the package, with
// AGENTRT_TEST_REQUIRED=1; it exits 0 only when every named test ran and
// passed (gaterun.Check). catalog prints, as JSON, each gate spec to upload
// (kind task_spec, its body byte for byte) and the set_done_rule changes that
// give each catalog row its gates in env: what the coordinating thread
// proposes as a plan change of the ARCH root. It sends nothing anywhere.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/gaterun"
)

type tests []string

func (t *tests) String() string     { return fmt.Sprint(*t) }
func (t *tests) Set(v string) error { *t = append(*t, v); return nil }

func main() {
	if len(os.Args) > 1 && os.Args[1] == "catalog" {
		os.Exit(catalog(os.Args[2:]))
	}
	fs := flag.NewFlagSet("agentrt-gate", flag.ContinueOnError)
	pkg := fs.String("pkg", "", "the package, ./internal/... with a trailing slash")
	var names tests
	fs.Var(&names, "test", "a test the gate needs to run and pass (repeated)")
	if err := fs.Parse(os.Args[1:]); err != nil || *pkg == "" || len(names) == 0 || fs.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "usage: agentrt-gate -pkg <package> -test <name> [-test <name>...] | agentrt-gate catalog -env prod|stg")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(gaterun.Run(ctx, ".", gaterun.Command{Pkg: *pkg, Tests: names}, os.Stdout))
}

func catalog(args []string) int {
	fs := flag.NewFlagSet("catalog", flag.ContinueOnError)
	env := fs.String("env", "", "prod or stg")
	if err := fs.Parse(args); err != nil || (*env != "prod" && *env != "stg") || fs.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "usage: agentrt-gate catalog -env prod|stg")
		return 2
	}
	specs, err := gaterun.Specs()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	type spec struct {
		SHA256 string `json:"sha256"`
		Body   string `json:"body"`
	}
	out := struct {
		Root    string           `json:"root"`
		Reason  string           `json:"reason"`
		Specs   []spec           `json:"specs"`
		Changes []map[string]any `json:"changes"`
	}{Root: "ARCH", Reason: "the architecture rows get their first gate tests from the reviewed catalog (TD-397)"}
	for sum, body := range specs {
		out.Specs = append(out.Specs, spec{SHA256: sum, Body: string(body)})
	}
	sort.Slice(out.Specs, func(i, j int) bool { return out.Specs[i].SHA256 < out.Specs[j].SHA256 })
	for _, r := range gaterun.Catalog {
		rule, err := r.Rule(*env)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		out.Changes = append(out.Changes, map[string]any{"op": "set_done_rule", "node": gaterun.ArchRowPrefix + r.Label, "rule": json.RawMessage(rule)})
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
