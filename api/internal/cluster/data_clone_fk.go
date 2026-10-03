package cluster

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Selective clone TRUNCATEs the selected tables in the target and loads them from the
// source dump. TRUNCATE … CASCADE would also empty every table that references a selected
// table — directly or through another referencing table — and nothing would restore them.
// The platform does not know which tables an application has, so the dependency graph is
// read from the target's catalog (pg_constraint) and a selection that leaves any
// referencing table out is refused. The restore itself truncates without CASCADE, so a
// constraint added after the check still fails the restore instead of emptying a table.

// dataCloneFKEdgesSQL lists every foreign key in the database as child → parent.
// conparentid = 0 keeps the declared constraint and drops the copies Postgres makes on
// each partition, so a partitioned table appears once under its own name.
const dataCloneFKEdgesSQL = `
SELECT cn.nspname || '.' || cc.relname, pn.nspname || '.' || pc.relname, c.conname
FROM pg_constraint c
JOIN pg_class cc ON cc.oid = c.conrelid
JOIN pg_namespace cn ON cn.oid = cc.relnamespace
JOIN pg_class pc ON pc.oid = c.confrelid
JOIN pg_namespace pn ON pn.oid = pc.relnamespace
WHERE c.contype = 'f' AND c.conparentid = 0
ORDER BY 1, 2, 3
`

// dataCloneFKEdge is one foreign key: Child references Parent. Table names are
// schema-qualified (schema.table).
type dataCloneFKEdge struct {
	Child      string `json:"table"`
	Parent     string `json:"references"`
	Constraint string `json:"constraint"`
}

// dataCloneQualify maps a selective-clone table name to its schema-qualified form.
// Selective clone only takes bare identifiers, which live in public.
func dataCloneQualify(table string) string {
	return "public." + table
}

// dataCloneSelectableName is the inverse of dataCloneQualify: the name a caller would put in
// tables[] to select this table, or the qualified name when it is outside public and so
// cannot be selected.
func dataCloneSelectableName(qualified string) string {
	if name, ok := strings.CutPrefix(qualified, "public."); ok {
		return name
	}
	return qualified
}

func parseDataCloneFKEdges(out string) ([]dataCloneFKEdge, error) {
	var edges []dataCloneFKEdge
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			return nil, fmt.Errorf("unreadable foreign-key row %q", line)
		}
		edges = append(edges, dataCloneFKEdge{Child: parts[0], Parent: parts[1], Constraint: parts[2]})
	}
	return edges, nil
}

// dataCloneUnselectedDependents walks the FK graph from the selected tables to every table
// that references them, directly or through another referencing table, and returns, for
// each such table outside the selection, the constraint through which it was first reached.
// These are the tables TRUNCATE … CASCADE would empty without restoring. Selected names are
// schema-qualified. The result is sorted by table name.
func dataCloneUnselectedDependents(selected []string, edges []dataCloneFKEdge) []dataCloneFKEdge {
	inSelection := make(map[string]bool, len(selected))
	for _, t := range selected {
		inSelection[t] = true
	}
	referencedBy := map[string][]dataCloneFKEdge{}
	for _, e := range edges {
		if e.Child == e.Parent {
			continue // a self-reference never reaches another table
		}
		referencedBy[e.Parent] = append(referencedBy[e.Parent], e)
	}
	for _, refs := range referencedBy {
		sort.Slice(refs, func(i, j int) bool {
			if refs[i].Child != refs[j].Child {
				return refs[i].Child < refs[j].Child
			}
			return refs[i].Constraint < refs[j].Constraint
		})
	}

	queue := append([]string{}, selected...)
	sort.Strings(queue)
	visited := map[string]bool{}
	for _, t := range queue {
		visited[t] = true
	}
	var missing []dataCloneFKEdge
	for len(queue) > 0 {
		table := queue[0]
		queue = queue[1:]
		for _, e := range referencedBy[table] {
			if visited[e.Child] {
				continue
			}
			visited[e.Child] = true
			queue = append(queue, e.Child)
			if !inSelection[e.Child] {
				missing = append(missing, e)
			}
		}
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i].Child < missing[j].Child })
	return missing
}

// ErrCloneFKClosure refuses a selective clone whose tables are referenced by tables that
// were not selected.
type ErrCloneFKClosure struct {
	Target  string
	Missing []dataCloneFKEdge
}

// MissingTables names the unselected referencing tables as tables[] would take them.
func (e *ErrCloneFKClosure) MissingTables() []string {
	out := make([]string, 0, len(e.Missing))
	for _, m := range e.Missing {
		out = append(out, dataCloneSelectableName(m.Child))
	}
	return out
}

func (e *ErrCloneFKClosure) Error() string {
	refs := make([]string, 0, len(e.Missing))
	outsidePublic := false
	for _, m := range e.Missing {
		refs = append(refs, fmt.Sprintf("%s (references %s via %s)", m.Child, m.Parent, m.Constraint))
		if !strings.HasPrefix(m.Child, "public.") {
			outsidePublic = true
		}
	}
	advice := "add them to tables[] to copy them in the same transaction, or use mode=full"
	if outsidePublic {
		advice = "selective clone only copies public tables, so use mode=full"
	}
	return fmt.Sprintf("selective clone refused: in %s the selected tables are referenced by %d table(s) that were not selected and would be emptied without being restored: %s; %s",
		e.Target, len(e.Missing), strings.Join(refs, ", "), advice)
}

// checkSelectiveFKClosure refuses the selection unless every table that references a
// selected table, transitively, is selected too — in every target, before any target is
// touched.
func (s *Service) checkSelectiveFKClosure(ctx context.Context, primary string, targets, tables []string) error {
	selected := make([]string, 0, len(tables))
	for _, t := range tables {
		selected = append(selected, dataCloneQualify(t))
	}
	for _, target := range targets {
		out, err := s.execOnPrimary(ctx, primary, "psql", "-U", "postgres", "-d", target, "-X", "-tA", "-F", "\t", "-c",
			dataCloneFKEdgesSQL)
		if err != nil {
			return fmt.Errorf("selective clone foreign-key check in %s failed: %w", target, err)
		}
		edges, err := parseDataCloneFKEdges(out)
		if err != nil {
			return fmt.Errorf("selective clone foreign-key check in %s: %w", target, err)
		}
		if missing := dataCloneUnselectedDependents(selected, edges); len(missing) > 0 {
			return &ErrCloneFKClosure{Target: target, Missing: missing}
		}
	}
	return nil
}
