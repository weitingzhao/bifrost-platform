package codeguard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Every table of the platform's agentrt schema has a row in the ownership
// table's "tables" section ("agentrt/<table>", or "agentrt" for the whole
// schema) before a migration creates it.
//
// Origin: 2026-11-22, the Owner in the Foundation thread ("register with me
// before you do the next thing, or many people lose track"; LAW-10). That
// day the ownership table had no tables section at all and the map found the
// platform's tables only by scanning the database afterwards; 18 tables were
// live on PROD and 2 were named anywhere in the table. They were registered
// the same day, so the baseline is empty.

var (
	sqlComment = regexp.MustCompile(`--[^\n]*`)
	createRE   = regexp.MustCompile(`(?i)create\s+table\s+(?:if\s+not\s+exists\s+)?(?:agentrt\.)?([a-z_0-9]+)`)
	dropRE     = regexp.MustCompile(`(?i)drop\s+table\s+(?:if\s+exists\s+)?([^;]+);`)
	dropEmpty  = regexp.MustCompile(`(?i)drop_table_if_empty\('(?:agentrt\.)?([a-z_0-9]+)'\)`)
	renameRE   = regexp.MustCompile(`(?i)alter\s+table\s+(?:agentrt\.)?([a-z_0-9]+)\s+rename\s+to\s+([a-z_0-9]+)`)
)

// liveTables replays migration files in order: a table is live once created
// and until it is dropped (DROP TABLE, possibly a list, or the
// drop_table_if_empty helper) or renamed away.
func liveTables(files map[string]string) []string {
	var names []string
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	live := map[string]bool{}
	for _, n := range names {
		sql := sqlComment.ReplaceAllString(files[n], "")
		type ev struct {
			at   int
			kind string
			a, b string
		}
		var evs []ev
		for _, m := range createRE.FindAllStringSubmatchIndex(sql, -1) {
			evs = append(evs, ev{m[0], "create", sql[m[2]:m[3]], ""})
		}
		for _, m := range dropRE.FindAllStringSubmatchIndex(sql, -1) {
			for _, part := range strings.Split(sql[m[2]:m[3]], ",") {
				if f := strings.Fields(part); len(f) > 0 {
					evs = append(evs, ev{m[0], "drop", strings.TrimPrefix(strings.ToLower(f[0]), "agentrt."), ""})
				}
			}
		}
		for _, m := range dropEmpty.FindAllStringSubmatchIndex(sql, -1) {
			evs = append(evs, ev{m[0], "drop", sql[m[2]:m[3]], ""})
		}
		for _, m := range renameRE.FindAllStringSubmatchIndex(sql, -1) {
			evs = append(evs, ev{m[0], "rename", sql[m[2]:m[3]], sql[m[4]:m[5]]})
		}
		sort.SliceStable(evs, func(i, j int) bool { return evs[i].at < evs[j].at })
		for _, e := range evs {
			switch e.kind {
			case "create":
				live[e.a] = true
			case "drop":
				delete(live, e.a)
			case "rename":
				if live[e.a] {
					delete(live, e.a)
					live[e.b] = true
				}
			}
		}
	}
	var out []string
	for t := range live {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// unregisteredTables names the live tables no row covers.
func unregisteredTables(live []string, rows map[string]json.RawMessage) []string {
	var out []string
	for _, t := range live {
		if _, ok := rows["agentrt/"+t]; ok {
			continue
		}
		if _, ok := rows["agentrt"]; ok {
			continue
		}
		out = append(out, t)
	}
	return out
}

func TestEveryPlatformTableHasARow(t *testing.T) {
	dir := filepath.Join("..", "agentrt", "migrations")
	paths, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil || len(paths) < 30 {
		t.Fatalf("migrations: %d files, %v", len(paths), err)
	}
	files := map[string]string{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		files[filepath.Base(p)] = string(b)
	}
	// The migration ledger is created by package migrate, not by a file.
	live := append(liveTables(files), "schema_migration")
	sort.Strings(live)
	var doc struct {
		Tables map[string]json.RawMessage `json:"tables"`
	}
	if err := json.Unmarshal(ownershipRaw(t), &doc); err != nil {
		t.Fatal(err)
	}
	t.Logf("agentrt tables live by the migrations: %d", len(live))
	for _, name := range unregisteredTables(live, doc.Tables) {
		t.Errorf("agentrt.%s has no row in the ownership table's tables section: add \"agentrt/%s\" with its one domain and the reason in the same commit as the migration (laws 72, 75; the map line owns the table)", name, name)
	}
}

// The control: replaying create, a dropped list, the drop helper and a
// rename leaves exactly the tables still there, and a live table without a
// row is named.
func TestLiveTablesReplaysTheMigrations(t *testing.T) {
	got := liveTables(map[string]string{
		"0001.sql": "CREATE TABLE agentrt.a (x int); CREATE TABLE agentrt.b (x int); CREATE TABLE c (x int); CREATE TABLE agentrt.d (x int);",
		"0002.sql": "-- CREATE TABLE agentrt.ghost\nDROP TABLE agentrt.a, agentrt.b; SELECT agentrt.drop_table_if_empty('agentrt.c'); ALTER TABLE agentrt.d RENAME TO e;",
	})
	if strings.Join(got, ",") != "e" {
		t.Fatalf("live = %q, want [e]", got)
	}
	if miss := unregisteredTables([]string{"e", "f"}, map[string]json.RawMessage{"agentrt/e": nil}); strings.Join(miss, ",") != "f" {
		t.Fatalf("unregistered = %q, want [f]", miss)
	}
}
