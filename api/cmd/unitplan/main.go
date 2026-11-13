// unitplan prints CI's plan of one commit of a local clone, as JSON: every
// unit's task with its key, computed from the commit's tree and its
// bifrost.units.yaml alone (no checkout, no go command; git objects are read
// through the git command). A pinned repository is read from the local clone
// -pin names. With -packages it prints instead every Go package of a module
// with its files, imports and library key.
//
//	unitplan -repo . -commit HEAD -pin bifrost-ui=../bifrost-ui -pin bifrost-trade-infra=../bifrost-trade-infra
//	unitplan -repo . -commit HEAD -module api -packages
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

func main() {
	repo := flag.String("repo", ".", "a local clone")
	commit := flag.String("commit", "HEAD", "the commit to read")
	module := flag.String("module", "api", "the Go module directory, from the repository root")
	packages := flag.Bool("packages", false, "print the module's packages")
	name := flag.String("name", "bifrost-platform", "the repository's name in the plan")
	manifest := flag.String("manifest", "", "a manifest file to plan with instead of the commit's own (a commit from before it had one)")
	pins := map[string]string{}
	flag.Func("pin", "name=path: the local clone a pinned repository is read from (repeat)", func(v string) error {
		n, p, ok := strings.Cut(v, "=")
		if !ok || n == "" || p == "" {
			return fmt.Errorf("want name=path")
		}
		pins[n] = p
		return nil
	})
	flag.Parse()
	if err := run(*repo, *commit, *module, *packages, *name, *manifest, pins); err != nil {
		fmt.Fprintln(os.Stderr, "unitplan:", err)
		os.Exit(1)
	}
}

func run(repo, commit, module string, packages bool, name, manifest string, pins map[string]string) error {
	out, err := exec.Command("git", "-C", repo, "rev-parse", commit+"^{commit}").Output()
	if err != nil {
		return fmt.Errorf("git rev-parse %s: %w", commit, err)
	}
	commit = strings.TrimSpace(string(out))
	tree, closeTree, err := gitTree(repo, commit)
	if err != nil {
		return err
	}
	defer closeTree()
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", " ")
	if !packages {
		var closers []func()
		defer func() {
			for _, c := range closers {
				c()
			}
		}()
		src := unitplan.Source{Pinned: func(pinned, at string) (*unitplan.Tree, error) {
			clone, ok := pins[pinned]
			if !ok {
				return nil, fmt.Errorf("no -pin %s=<clone>", pinned)
			}
			t, c, err := gitTree(clone, at)
			if err == nil {
				closers = append(closers, c)
			}
			return t, err
		}}
		var plan *unitplan.Plan
		if manifest != "" {
			doc, err := os.ReadFile(manifest)
			if err != nil {
				return err
			}
			plan, err = unitplan.BuildWith(name, commit, tree, src, doc)
			if err != nil {
				return err
			}
		} else if plan, err = unitplan.Build(name, commit, tree, src); err != nil {
			return err
		}
		return enc.Encode(plan)
	}
	mod, err := unitplan.ReadGoModule(tree, module)
	if err != nil {
		return err
	}
	return enc.Encode(map[string]any{"commit": commit, "module": mod.Path, "go_mod": mod.GoMod, "go_sum": mod.GoSum, "packages": mod.Packages})
}

// gitTree reads a commit's whole tree with git ls-tree and its blobs, as
// they are asked for, from one git cat-file --batch process.
func gitTree(repo, commit string) (*unitplan.Tree, func(), error) {
	root, err := exec.Command("git", "-C", repo, "rev-parse", commit+"^{tree}").Output()
	if err != nil {
		return nil, nil, fmt.Errorf("git rev-parse %s^{tree} in %s: %w", commit, repo, err)
	}
	out, err := exec.Command("git", "-C", repo, "ls-tree", "-r", "-t", "-z", commit).Output()
	if err != nil {
		return nil, nil, fmt.Errorf("git ls-tree %s: %w", commit, err)
	}
	var entries []unitplan.Entry
	for _, rec := range strings.Split(string(out), "\x00") {
		if rec == "" {
			continue
		}
		meta, path, ok := strings.Cut(rec, "\t")
		f := strings.Fields(meta)
		if !ok || len(f) != 3 {
			return nil, nil, fmt.Errorf("git ls-tree: unexpected record %q", rec)
		}
		if f[1] != "blob" && f[1] != "tree" {
			continue // a submodule: not content of this repository
		}
		entries = append(entries, unitplan.Entry{Path: path, Dir: f[1] == "tree", ID: f[2]})
	}
	cat := exec.Command("git", "-C", repo, "cat-file", "--batch")
	stdin, err := cat.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	stdout, err := cat.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err = cat.Start(); err != nil {
		return nil, nil, err
	}
	r := bufio.NewReader(stdout)
	read := func(id string) ([]byte, error) {
		if _, err := io.WriteString(stdin, id+"\n"); err != nil {
			return nil, err
		}
		header, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		f := strings.Fields(header)
		if len(f) != 3 || f[1] != "blob" {
			return nil, fmt.Errorf("git cat-file %s: %s", id, strings.TrimSpace(header))
		}
		n, err := strconv.Atoi(f[2])
		if err != nil {
			return nil, err
		}
		body := make([]byte, n+1) // and the newline after it
		if _, err = io.ReadFull(r, body); err != nil {
			return nil, err
		}
		return body[:n], nil
	}
	tree, err := unitplan.NewTree(strings.TrimSpace(string(root)), entries, read)
	return tree, func() { _ = stdin.Close(); _ = cat.Wait() }, err
}
