// debtimport reads files and git history only. JSON goes to stdout and the
// review report to stderr; acceptance text is never executed.
package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/debtimport"
)

//go:embed projects.json
var defaultProjects []byte

type catalog struct {
	Projects    []debtimport.Project `json:"projects"`
	CrewProject string               `json:"crew_project"`
	RepoRoots   map[string][]string  `json:"repo_roots"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr, time.Now); err != nil {
		fmt.Fprintln(os.Stderr, "debtimport:", err)
		os.Exit(1)
	}
}
func run(args []string, out, report io.Writer, now func() time.Time) error {
	if len(args) == 0 {
		return errors.New("usage: debtimport parse <ledger.md> <ratchets.md> [--infra path] [--repos workspace] [--reserved TD-n,TD-m|none] [--at RFC3339] [--projects file.json] | export <request.json> [--file ledger|ratchets|both]")
	}
	command := args[0]
	pos, flags, err := arguments(args[1:])
	if err != nil {
		return err
	}
	switch command {
	case "parse":
		if len(pos) != 2 {
			return errors.New("parse needs a ledger and a ratchets file")
		}
		if flags["file"] != "" {
			return errors.New("--file is only for export")
		}
		ledger, err := os.ReadFile(pos[0])
		if err != nil {
			return err
		}
		ratchets, err := os.ReadFile(pos[1])
		if err != nil {
			return err
		}
		at := now().UTC()
		if flags["at"] != "" {
			at, err = time.Parse(time.RFC3339, flags["at"])
			if err != nil {
				return fmt.Errorf("--at: %w", err)
			}
		}
		data := defaultProjects
		if flags["projects"] != "" {
			data, err = os.ReadFile(flags["projects"])
			if err != nil {
				return err
			}
		}
		var c catalog
		if err = decode(data, &c); err != nil {
			return fmt.Errorf("projects: %w", err)
		}
		opts := debtimport.Options{ImportedAt: at, Projects: c.Projects, CrewProject: c.CrewProject, RepoRoots: c.RepoRoots}
		if flags["infra"] != "" {
			history, err := newGitHistory(flags["infra"])
			if err != nil {
				return err
			}
			opts.History = history
			root := flags["repos"]
			if root == "" {
				root = filepath.Dir(flags["infra"])
			}
			opts.FloorSources, err = collectFloors(history, root, flags["reserved"])
			if err != nil {
				return err
			}
		}
		if flags["infra"] == "" && (flags["reserved"] != "" || flags["repos"] != "") {
			return errors.New("floor options require --infra")
		}
		r, review, err := debtimport.Parse(ledger, ratchets, opts)
		if err != nil {
			return err
		}
		requestJSON, err := r.JSON()
		if err != nil {
			return err
		}
		reportJSON, err := review.JSON()
		if err != nil {
			return err
		}
		if _, err = fmt.Fprintln(out, string(requestJSON)); err != nil {
			return err
		}
		_, err = fmt.Fprintln(report, string(reportJSON))
		return err
	case "export":
		if len(pos) != 1 {
			return errors.New("export needs one request JSON file")
		}
		if flags["infra"] != "" || flags["at"] != "" || flags["projects"] != "" || flags["repos"] != "" || flags["reserved"] != "" {
			return errors.New("export only accepts --file")
		}
		data, err := os.ReadFile(pos[0])
		if err != nil {
			return err
		}
		var r debtimport.Request
		if err = decode(data, &r); err != nil {
			return err
		}
		ledger, ratchets, err := debtimport.Export(r)
		if err != nil {
			return err
		}
		switch flags["file"] {
		case "ledger":
			_, err = out.Write(ledger)
		case "ratchets":
			_, err = out.Write(ratchets)
		case "", "both":
			if _, err = out.Write(ledger); err != nil {
				return err
			}
			if _, err = fmt.Fprint(out, "\n<!-- RATCHETS.md -->\n\n"); err != nil {
				return err
			}
			_, err = out.Write(ratchets)
		default:
			return errors.New("--file must be ledger, ratchets or both")
		}
		return err
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}
func arguments(args []string) ([]string, map[string]string, error) {
	pos := []string{}
	flags := map[string]string{}
	for i := 0; i < len(args); i++ {
		if !strings.HasPrefix(args[i], "--") {
			pos = append(pos, args[i])
			continue
		}
		key, value, inline := strings.Cut(strings.TrimPrefix(args[i], "--"), "=")
		if key != "infra" && key != "at" && key != "projects" && key != "file" && key != "repos" && key != "reserved" {
			return nil, nil, fmt.Errorf("unknown option --%s", key)
		}
		if _, ok := flags[key]; ok {
			return nil, nil, fmt.Errorf("duplicate option --%s", key)
		}
		if !inline {
			i++
			if i == len(args) {
				return nil, nil, fmt.Errorf("--%s needs a value", key)
			}
			value = args[i]
		}
		if value == "" {
			return nil, nil, fmt.Errorf("--%s needs a value", key)
		}
		flags[key] = value
	}
	return pos, flags, nil
}
func decode(data []byte, v any) error {
	d := json.NewDecoder(strings.NewReader(string(data)))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("expected exactly one JSON value")
	}
	return nil
}
