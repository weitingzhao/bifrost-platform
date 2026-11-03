package rules

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// The projects technical debt is filed under (bifrost-trade-infra
// agent-config/work/multi-agent/DESIGN-tech-debt-runtime-2026-11-01.md §4.1,
// T3; the Owner chose A on 2026-11-01): a project key is an opaque string
// the platform compares and never interprets, and the list lives in this
// signed rule, so no product name is written in the platform's code. Each
// environment files debt only while its own rule is in force and reads
// (§2.4). What owns says only suggests a project; the project of a debt is
// its own field.
//
//	rule_id: debt-projects
//	projects:
//	  - {key: crew, name: "Crew", group: crew, owns: ["bifrost-platform/**"]}
//	  - {key: example, name: "Example", group: product, owns: ["example/**"], text_scan: strict}
//	acceptance_runners: {}   # repository -> CI test path prefixes (§6.4)
//	budgets: {}              # not read yet (§9)
const KindDebtProjects = "debt-projects"

// TextScanStrict marks a project whose debt texts are refused when they
// carry amounts or long runs of digits (T13).
const TextScanStrict = "strict"

// MaxDebtProjects bounds how many projects one rule lists.
const MaxDebtProjects = 64

// debtKeyRE is a project or group key (the debt table's check).
var debtKeyRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// DebtProject is one project of the rule.
type DebtProject struct {
	Key, Name, Group string
	Owns             []string // path patterns, as written
	TextScan         string   // "" or TextScanStrict
}

// DebtProjects is a debt-projects rule's part: its projects sorted by key,
// and the CI test path prefixes each repository's acceptance may name.
type DebtProjects struct {
	Projects          []DebtProject
	AcceptanceRunners map[string][]string
}

// Project is the project of key, or nil.
func (d *DebtProjects) Project(key string) *DebtProject {
	for i := range d.Projects {
		if d.Projects[i].Key == key {
			return &d.Projects[i]
		}
	}
	return nil
}

func (r *Rule) debtProjects(o *object) error {
	d := &DebtProjects{AcceptanceRunners: map[string][]string{}}
	seen := map[string]bool{}
	err := o.list("projects", false, func(n *yaml.Node, where string) error {
		p, err := mapping(n, where)
		if err != nil {
			return err
		}
		var dp DebtProject
		if dp.Key, err = p.str("key", false); err != nil {
			return err
		}
		if dp.Name, err = p.str("name", false); err != nil {
			return err
		}
		if dp.Group, err = p.str("group", false); err != nil {
			return err
		}
		if dp.Owns, err = p.strings("owns", false); err != nil {
			return err
		}
		if dp.TextScan, err = p.str("text_scan", true); err != nil {
			return err
		}
		switch {
		case !debtKeyRE.MatchString(dp.Key) || !debtKeyRE.MatchString(dp.Group):
			return fmt.Errorf("%s: key and group are keys (%s)", where, debtKeyRE)
		case strings.TrimSpace(dp.Name) == "":
			return fmt.Errorf("%s: name is not empty", where)
		case dp.TextScan != "" && dp.TextScan != TextScanStrict:
			return fmt.Errorf("%s.text_scan is %q or absent", where, TextScanStrict)
		case seen[dp.Key]:
			return fmt.Errorf("%s: project %s is listed twice", where, dp.Key)
		}
		if dp.Owns == nil {
			dp.Owns = []string{}
		}
		seen[dp.Key] = true
		d.Projects = append(d.Projects, dp)
		return p.done()
	})
	if err != nil {
		return err
	}
	if len(d.Projects) == 0 || len(d.Projects) > MaxDebtProjects {
		return fmt.Errorf("%s.projects lists 1 to %d projects", o.where, MaxDebtProjects)
	}
	slices.SortFunc(d.Projects, func(a, b DebtProject) int { return strings.Compare(a.Key, b.Key) })
	if n, err := o.node("acceptance_runners", true); err != nil {
		return err
	} else if n != nil {
		runners, err := mapping(n, o.where+".acceptance_runners")
		if err != nil {
			return err
		}
		for _, repo := range runners.order {
			if d.AcceptanceRunners[repo], err = runners.strings(repo, false); err != nil {
				return err
			}
		}
	}
	if n, err := o.node("budgets", true); err != nil {
		return err
	} else if n != nil {
		b, err := mapping(n, o.where+".budgets")
		if err != nil {
			return err
		}
		if len(b.order) > 0 {
			return errors.New("budgets is empty: no budget is read yet (design §9)")
		}
	}
	r.DebtProjects = d
	return nil
}

func (d *DebtProjects) doc() map[string]any {
	projects := make([]any, 0, len(d.Projects))
	for _, p := range d.Projects {
		pd := map[string]any{"key": p.Key, "name": p.Name, "group": p.Group, "owns": anyList(p.Owns)}
		if p.TextScan != "" {
			pd["text_scan"] = p.TextScan
		}
		projects = append(projects, pd)
	}
	runners := map[string]any{}
	for repo, prefixes := range d.AcceptanceRunners {
		runners[repo] = anyList(prefixes)
	}
	return map[string]any{"projects": projects, "acceptance_runners": runners, "budgets": map[string]any{}}
}
