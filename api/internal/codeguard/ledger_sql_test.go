package codeguard

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ledgerTable matches SQL that names the event ledger or the actor table
// directly (agentrt.event, agentrt.actor; not event_stream, event_id and the
// other tables whose names begin the same).
var ledgerTable = regexp.MustCompile(`agentrt\.(event|actor)([^_a-z]|$)`)

// ledgerOwners are the two packages the tables belong to. Everyone else
// writes through eventlog.Append and asks through the functions these two
// export; a package with its own SQL on them has to follow every change of
// the tables.
var ledgerOwners = map[string]bool{"agentrt/eventlog": true, "agentrt/actor": true}

// ledgerSQL is the number of lines, comments aside, of each other package's
// non-test files that still name one of the two tables. It only falls: a
// package over its number or missing from the list fails, and one under it
// fails until the number is lowered.
//
// Origin: 2026-11-16, an independent review of the foundation code (shared
// notes brain/foundation-code-quality-review-2026-11-16.md, the second of its
// three heaviest findings): fourteen packages read agentrt.event with SQL of
// their own and five inserted into agentrt.actor, two of them with the same
// sentences copied. By this count there were 83 such lines that day; the
// first replacement (the actor checks of the bus and of evidence, through
// actor.Exists and actor.RegisterController) left 73, and the second (the
// ledger's own Last, Has and HasBody) 67.
var ledgerSQL = map[string]int{
	"agentrt/agentrtapi":       5,
	"agentrt/artifact":         2,
	"agentrt/coord":            2,
	"agentrt/deliveryreadings": 1,
	"agentrt/evidence":         2,
	"agentrt/externalwork":     4,
	"agentrt/loopreadings":     1,
	"agentrt/planreadings":     11,
	"agentrt/planstore":        24,
	"agentrt/seenfacts":        2,
	"agentrt/store":            4,
	"agentrt/workregistration": 5,
	"assessmentreadings":       1,
	"bus/relay":                1,
}

// countLedgerSQL counts, by package directory under root, the lines of
// non-test Go files that name a ledger table outside a comment.
func countLedgerSQL(root string) (map[string]int, error) {
	found := map[string]int{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		n := 0
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if ledgerTable.MatchString(line) {
				n++
			}
		}
		if n > 0 {
			rel, _ := filepath.Rel(root, filepath.Dir(path))
			found[filepath.ToSlash(rel)] += n
		}
		return nil
	})
	return found, err
}

func TestLedgerTablesAreReadThroughTheirPackages(t *testing.T) {
	found, err := countLedgerSQL("..")
	if err != nil {
		t.Fatal(err)
	}
	for owner := range ledgerOwners {
		if found[owner] == 0 {
			t.Fatalf("the check does not see the owner %s's own SQL", owner)
		}
		delete(found, owner)
	}
	pkgs := map[string]bool{}
	for p := range found {
		pkgs[p] = true
	}
	for p := range ledgerSQL {
		pkgs[p] = true
	}
	names := make([]string, 0, len(pkgs))
	for p := range pkgs {
		names = append(names, p)
	}
	sort.Strings(names)
	for _, p := range names {
		got, allowed := found[p], ledgerSQL[p]
		switch {
		case got > allowed:
			t.Errorf("%s: %d lines of SQL on agentrt.event or agentrt.actor, allowed %d — write through eventlog.Append and ask eventlog or actor for a function", p, got, allowed)
		case got < allowed:
			t.Errorf("%s: %d lines, allowed %d — lower the number (it only falls)", p, got, allowed)
		}
	}
}

// The positive control: on a made-up tree the count sees SQL on the two
// tables, and not a comment, a test file, or a table whose name only begins
// the same.
func TestLedgerSQLCountSeesWhatItShould(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a/a.go", "package a\nconst q = `SELECT 1 FROM agentrt.event WHERE x`\nconst r = `INSERT INTO agentrt.actor (a)`\n// agentrt.event in a comment\n")
	write("a/a_test.go", "package a\nconst q = `SELECT 1 FROM agentrt.event`\n")
	write("b/b.go", "package b\nconst q = `SELECT 1 FROM agentrt.event_stream`\nconst r = `SELECT 1 FROM agentrt.actor_x`\n")
	got, err := countLedgerSQL(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["a"] != 2 {
		t.Fatalf("count = %v, want a:2 only", got)
	}
}

// ledgerWrite matches SQL that writes one of the two tables.
var ledgerWrite = regexp.MustCompile(`(?i)(INSERT\s+INTO|UPDATE|DELETE\s+FROM)\s+agentrt\.(event|actor)([^_a-z]|$)`)

// ledgerWrites are the lines outside the two owners that write a ledger
// table. None writes agentrt.event (the table also refuses it: it is
// append-only by trigger); two still insert an actor of a kind the actor
// package has no function for yet (a seat, the seeded actors). It only falls.
//
// Origin: 2026-11-16, the coordinator's ruling on the ledger's contract: the
// write is one package's, the read is by the declared shape
// (eventlog.ReadShape).
var ledgerWrites = map[string]int{
	"agentrt/coord": 1,
	"agentrt/store": 1,
}

func TestLedgerTablesAreWrittenByTheirPackagesOnly(t *testing.T) {
	found := map[string]int{}
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel("..", filepath.Dir(path))
		pkg := filepath.ToSlash(rel)
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if m := ledgerWrite.FindStringSubmatch(line); m != nil {
				if m[2] == "event" && !ledgerOwners[pkg] {
					t.Errorf("%s writes agentrt.event in SQL of its own: %s — only eventlog.Append writes the ledger", pkg, strings.TrimSpace(line))
				}
				found[pkg]++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// The positive control: the owners' own writes are seen.
	if found["agentrt/eventlog"] == 0 || found["agentrt/actor"] == 0 {
		t.Fatalf("the check does not see the owners' own writes: %v", found)
	}
	delete(found, "agentrt/eventlog")
	delete(found, "agentrt/actor")
	for pkg, n := range found {
		if n > ledgerWrites[pkg] {
			t.Errorf("%s: %d lines write a ledger table, allowed %d — ask the actor package for a function", pkg, n, ledgerWrites[pkg])
		}
	}
	for pkg, allowed := range ledgerWrites {
		if found[pkg] < allowed {
			t.Errorf("%s: %d lines, allowed %d — lower the number (it only falls)", pkg, found[pkg], allowed)
		}
	}
}
