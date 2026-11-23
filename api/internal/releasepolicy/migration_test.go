package releasepolicy_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

const (
	migrationDir  = "db/migrations"
	firstFile     = migrationDir + "/0001_core.sql"
	secondFile    = migrationDir + "/0002_notes.sql"
	firstSQL      = "CREATE TABLE orders (id bigint PRIMARY KEY);\n"
	inClassSQL    = "CREATE TABLE notes (note_id bigint PRIMARY KEY);\nALTER TABLE orders ADD COLUMN note_id bigint;\n"
	sourcePipe    = "deliver-app-stg"
	ownPipe       = "deliver-pinned"
	reversibleWhy = "is not a new file that cannot destroy rows (ask the Owner): "
)

func sum(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])
}

// reversible is the fixture's policy with the two fields the Owner signs to
// let a migration that cannot destroy rows through.
func reversible(doc map[string]any) {
	doc["conditions"].(map[string]any)["ddl_reversible"] = true
	doc["ddl_reversible_paths"] = []map[string]any{{"repo": "repo-app", "paths": []string{migrationDir + "/*.sql"}}}
	doc["allow"] = []string{pipeline, ownPipe}
}

// migrationWorld: the release of repo-app adds the migration 0002 with the
// text second; and for run_agentrt_migration, this environment is the one
// deliver-pinned delivers, it runs the old commit and has applied 0001, and
// the source pipeline delivered the new commit successfully.
func migrationWorld(f *applyFixture, second string) {
	oldSHA, newSHA := rptest.SHA("old"), rptest.SHA("new")
	f.facts.Files["repo-app"] = []string{"docs/readme.md", secondFile}
	f.facts.Text["repo-app@"+oldSHA+":"+firstFile] = firstSQL
	f.facts.Text["repo-app@"+newSHA+":"+firstFile] = firstSQL
	f.facts.Text["repo-app@"+newSHA+":"+secondFile] = second
	f.facts.Dirs["repo-app@"+newSHA+":"+migrationDir] = []string{"0001_core.sql", "0002_notes.sql", "embed.go"}
	f.facts.Records[ownPipe] = map[string]string{"repo-app": oldSHA, "repo-lib": rptest.SHA("old-lib")}
	f.facts.Records[sourcePipe] = map[string]string{"repo-app": newSHA, "repo-lib": rptest.SHA("new-lib")}
	f.migs.State = releasepolicy.OwnMigrations{Pipeline: ownPipe, Repo: "repo-app", Dir: migrationDir, Applied: map[int]string{1: sum(firstSQL)}}
}

func (f *applyFixture) migrate() releasepolicy.Decision {
	return f.eng.Decide(context.Background(), "run_agentrt_migration", releasepolicy.Tier("C"), map[string]any{"commit": rptest.SHA("new")})
}

// Owner 2026-11-15: a migration that cannot destroy rows reaches PROD without
// a card; the platform runs it (cards #493 #554 #558 were Owner-run Jobs).
// A release whose diff carries a new migration of the class is covered: the
// PROD pipeline's own schema task refuses a build whose migrations are not
// applied, so the release cannot run ahead of the migration.
func TestReleaseWithAReversibleMigrationIsCovered(t *testing.T) {
	f := newApplyFixture(t, reversible)
	migrationWorld(f, inClassSQL)
	d := f.decide(nil)
	wantAuto(t, d)
	if strings.Join(d.ReversibleDDL, ",") != "repo-app:"+secondFile || !strings.Contains(d.ClauseText(), ",ddl_reversible,") ||
		strings.Contains(d.ClauseText(), ",no_ddl") || len(d.AdditiveDDL) != 0 {
		t.Fatalf("reversible = %q additive = %q clauses = %s", d.ReversibleDDL, d.AdditiveDDL, d.ClauseText())
	}
}

// Origin: 2026-11-21, card #674. The release 3c335db2..cc59bf9f needed the
// Owner only because migration 0042 (a DROP) was in its diff, though card #671
// had already run 0042 on PROD: one deletion approved twice. A migration the
// environment this pipeline delivers has applied with the same bytes runs
// nothing on release, so it does not hold the release; anything else about
// it (other bytes, another pipeline's environment, not applied, unread) is
// judged as before.
func TestReleaseCarryingAMigrationAlreadyAppliedHere(t *testing.T) {
	const dropSQL = "DROP TABLE orders;\n"
	for name, c := range map[string]struct {
		world   func(f *applyFixture)
		covered bool
	}{
		"applied here with the same bytes": {world: func(f *applyFixture) {
			f.migs.State.Pipeline = pipeline
			f.migs.State.Applied[2] = sum(dropSQL)
		}, covered: true},
		"applied here with other bytes": {world: func(f *applyFixture) {
			f.migs.State.Pipeline = pipeline
			f.migs.State.Applied[2] = sum("DROP TABLE notes;\n")
		}},
		"applied on another pipeline's environment": {world: func(f *applyFixture) {
			f.migs.State.Applied[2] = sum(dropSQL)
		}},
		"not applied": {world: func(f *applyFixture) {
			f.migs.State.Pipeline = pipeline
		}},
		"this environment's migrations cannot be read": {world: func(f *applyFixture) {
			f.migs.Err = errors.New("database down")
		}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newApplyFixture(t, reversible)
			migrationWorld(f, dropSQL)
			c.world(f)
			d := f.decide(nil)
			if c.covered {
				wantAuto(t, d)
				if strings.Join(d.AppliedDDL, ",") != "repo-app:"+secondFile || !strings.Contains(d.ClauseText(), ",migrations_applied,") ||
					len(d.ReversibleDDL) != 0 || strings.Contains(d.ClauseText(), ",no_ddl") {
					t.Fatalf("applied = %q reversible = %q clauses = %s", d.AppliedDDL, d.ReversibleDDL, d.ClauseText())
				}
				return
			}
			if d.Auto || !strings.Contains(strings.Join(d.Reasons, "\n"), reversibleWhy+secondFile) || len(d.AppliedDDL) != 0 {
				t.Fatalf("auto = %v applied = %q reasons = %q", d.Auto, d.AppliedDDL, d.Reasons)
			}
		})
	}
}

// Owner 2026-11-15: a migration that cannot destroy rows reaches PROD without
// a card; the platform runs it (cards #493 #554 #558 were Owner-run Jobs).
// Each case is the covered release above with one thing wrong; the fixture's
// policy has additive_ddl on, and none of these falls back to it.
func TestReleaseWithAMigrationOutOfClassIsRefused(t *testing.T) {
	oldSHA, newSHA := rptest.SHA("old"), rptest.SHA("new")
	for name, c := range map[string]struct {
		edit  func(doc map[string]any)
		world func(f *applyFixture)
		want  string
	}{
		"DROP TABLE": {world: func(f *applyFixture) { migrationWorld(f, "DROP TABLE orders;\n") },
			want: reversibleWhy + secondFile + ": statement 1: DROP TABLE is not a statement of the class"},
		"a DO block": {world: func(f *applyFixture) { migrationWorld(f, "DO $$ BEGIN DELETE FROM orders; END $$;\n") },
			want: secondFile + ": statement 1: DO is not"},
		"an existing migration with a line added, which the additive check would pass": {world: func(f *applyFixture) {
			migrationWorld(f, inClassSQL)
			f.facts.Files["repo-app"] = []string{firstFile}
			f.facts.Text["repo-app@"+newSHA+":"+firstFile] = firstSQL + "ALTER TABLE orders ADD COLUMN note text;\n"
		}, want: firstFile + " exists at " + oldSHA[:12] + ": it is changed or deleted"},
		"an existing migration deleted": {world: func(f *applyFixture) {
			migrationWorld(f, inClassSQL)
			f.facts.Files["repo-app"] = []string{firstFile}
			delete(f.facts.Text, "repo-app@"+newSHA+":"+firstFile)
		}, want: firstFile + " exists at " + oldSHA[:12]},
		"one file of the class and one not": {world: func(f *applyFixture) {
			migrationWorld(f, inClassSQL)
			f.facts.Files["repo-app"] = []string{secondFile, migrationDir + "/0003_gone.sql"}
			f.facts.Text["repo-app@"+newSHA+":"+migrationDir+"/0003_gone.sql"] = "TRUNCATE orders;\n"
		}, want: "0003_gone.sql: statement 1: TRUNCATE"},
		"a migration in another directory, of the class but not additive": {world: func(f *applyFixture) {
			migrationWorld(f, inClassSQL)
			f.facts.Files["repo-app"] = []string{"db/other/0001_x.sql"}
			f.facts.Text["repo-app@"+newSHA+":db/other/0001_x.sql"] = "DROP FUNCTION old_fn();\n"
		}, want: "changes DDL that is not additive-only (tier D, ask the Owner): db/other/0001_x.sql: adds DROP"},
		"another file of the migrations directory": {world: func(f *applyFixture) {
			migrationWorld(f, inClassSQL)
			f.facts.Files["repo-app"] = []string{secondFile, migrationDir + "/embed.go"}
			f.facts.Text["repo-app@"+oldSHA+":"+migrationDir+"/embed.go"] = "package migrations\n"
			f.facts.Text["repo-app@"+newSHA+":"+migrationDir+"/embed.go"] = "package other\n"
		}, want: "not additive-only"},
		"the condition without the paths": {edit: func(doc map[string]any) {
			reversible(doc)
			delete(doc, "ddl_reversible_paths")
			doc["conditions"].(map[string]any)["additive_ddl"] = false
		}, world: func(f *applyFixture) { migrationWorld(f, inClassSQL) }, want: "hits no_ddl: " + secondFile},
		"the paths without the condition": {edit: func(doc map[string]any) {
			reversible(doc)
			delete(doc["conditions"].(map[string]any), "ddl_reversible")
			doc["conditions"].(map[string]any)["additive_ddl"] = false
		}, world: func(f *applyFixture) { migrationWorld(f, inClassSQL) }, want: "hits no_ddl: " + secondFile},
		"the file cannot be read": {world: func(f *applyFixture) {
			migrationWorld(f, inClassSQL)
			delete(f.facts.Text, "repo-app@"+newSHA+":"+secondFile)
		}, want: secondFile + " does not exist at " + newSHA[:12]},
	} {
		t.Run(name, func(t *testing.T) {
			edit := c.edit
			if edit == nil {
				edit = reversible
			}
			f := newApplyFixture(t, edit)
			c.world(f)
			d := f.decide(nil)
			if d.Auto || !strings.Contains(strings.Join(d.Reasons, "\n"), c.want) {
				t.Fatalf("auto = %v, reasons = %q, want %q", d.Auto, d.Reasons, c.want)
			}
		})
	}
}

// Owner 2026-11-15: a migration that cannot destroy rows reaches PROD without
// a card; the platform runs it (cards #493 #554 #558 were Owner-run Jobs).
// The positive control of the action's cases: the commit was delivered to the
// source environment, and the one file this environment has not applied is
// new since the commit it runs and of the class.
func TestRunMigrationIsCovered(t *testing.T) {
	f := newApplyFixture(t, reversible)
	migrationWorld(f, inClassSQL)
	d := f.migrate()
	wantAuto(t, d)
	if d.PolicyID != "rp-apply" || d.Pipeline != ownPipe || d.SHAText() != "repo-app="+rptest.SHA("new") || strings.Join(d.ReversibleDDL, ",") != "repo-app:"+secondFile {
		t.Fatalf("decision = %+v", d)
	}
	if got, want := d.ClauseText(), "signature,unexpired,not_frozen,allow:"+ownPipe+",pinned_to:"+sourcePipe+",ddl_reversible"; got != want {
		t.Fatalf("clauses = %s, want %s", got, want)
	}
	// An older successful record of the source vouches as well as its newest.
	f.facts.Older = map[string][]releasepolicy.Shipment{sourcePipe: {{Repos: f.facts.Records[sourcePipe], Succeeded: true}}}
	f.facts.Records[sourcePipe] = map[string]string{"repo-app": rptest.SHA("later")}
	wantAuto(t, f.migrate())
	// Nothing to apply is covered too: the action then does nothing.
	f.migs.State.Applied[2] = sum(inClassSQL)
	if d = f.migrate(); !d.Auto || len(d.ReversibleDDL) != 0 {
		t.Fatalf("nothing pending: auto = %v reasons = %q files = %q", d.Auto, d.Reasons, d.ReversibleDDL)
	}
}

// Owner 2026-11-15: a migration that cannot destroy rows reaches PROD without
// a card; the platform runs it (cards #493 #554 #558 were Owner-run Jobs).
// Each case is the covered request above with one thing wrong: it waits for
// the Owner's card, and the reason names the file and the statement kind.
func TestRunMigrationNotCovered(t *testing.T) {
	oldSHA, newSHA := rptest.SHA("old"), rptest.SHA("new")
	frozen := map[string]string{"frozen": "true", "frozen_at": "2026-10-08T11:30:00Z", "who": "owner", "reason": "incident"}
	for name, c := range map[string]struct {
		edit   func(doc map[string]any)
		world  func(f *applyFixture)
		params map[string]any
		tier   releasepolicy.Tier
		want   string
	}{
		"a pending file out of the class": {world: func(f *applyFixture) {
			f.facts.Text["repo-app@"+newSHA+":"+secondFile] = "CREATE TABLE notes (id int);\nDROP TABLE orders;\n"
		}, want: "repo-app " + oldSHA[:12] + ".." + newSHA[:12] + " has a pending migration that " + reversibleWhy + secondFile + ": statement 2: DROP TABLE is not a statement of the class"},
		"never delivered to the source environment": {world: func(f *applyFixture) {
			f.facts.Records[sourcePipe] = map[string]string{"repo-app": rptest.SHA("other")}
		}, want: "was never delivered successfully by " + sourcePipe},
		"the source delivery did not succeed": {world: func(f *applyFixture) {
			f.facts.Older = map[string][]releasepolicy.Shipment{sourcePipe: {{Repos: f.facts.Records[sourcePipe]}}}
			f.facts.Records[sourcePipe] = map[string]string{"repo-app": rptest.SHA("other")}
		}, want: "did not succeed"},
		"a pending file this environment's own build already carries": {world: func(f *applyFixture) {
			f.facts.Text["repo-app@"+oldSHA+":"+secondFile] = inClassSQL
		}, want: secondFile + " exists at " + oldSHA[:12]},
		"two pending files, the first out of the class": {world: func(f *applyFixture) {
			f.migs.State.Applied = map[int]string{}
			f.facts.Text["repo-app@"+newSHA+":"+firstFile] = "INSERT INTO t VALUES (1);\n"
			delete(f.facts.Text, "repo-app@"+oldSHA+":"+firstFile)
		}, want: firstFile + ": statement 1: INSERT is not"},
		"an applied migration edited since": {world: func(f *applyFixture) {
			f.facts.Text["repo-app@"+newSHA+":"+firstFile] = firstSQL + "-- edited\n"
		}, want: "0001_core.sql ran on this environment with other bytes"},
		"the environment is ahead of the commit": {world: func(f *applyFixture) {
			f.migs.State.Applied[7] = sum("x")
		}, want: "has applied migrations [7], which " + newSHA[:12] + " does not carry"},
		"the migrations directory is outside the signed paths": {edit: func(doc map[string]any) {
			reversible(doc)
			doc["ddl_reversible_paths"] = []map[string]any{{"repo": "repo-app", "paths": []string{"db/elsewhere/*.sql"}}}
		}, want: "pending migrations outside the policy's ddl_reversible_paths: " + secondFile},
		"the paths are another repo's": {edit: func(doc map[string]any) {
			reversible(doc)
			doc["ddl_reversible_paths"] = []map[string]any{{"repo": "repo-lib", "paths": []string{migrationDir + "/*.sql"}}}
		}, want: "outside the policy's ddl_reversible_paths"},
		"no record says what this environment runs": {world: func(f *applyFixture) { delete(f.facts.Records, ownPipe) },
			want: "no release record of " + ownPipe},
		"this environment's pipeline is pinned to no source": {world: func(f *applyFixture) { f.migs.State.Pipeline = pipeline },
			want: "the policy pins " + pipeline + " to no source pipeline for repo-app"},
		"this environment's pipeline is not allowed": {edit: func(doc map[string]any) {
			reversible(doc)
			doc["allow"] = []string{pipeline}
		}, want: ownPipe + ", which delivers this environment, is not in the policy's allow list"},
		"this environment cannot be read": {world: func(f *applyFixture) { f.migs.Err = errors.New("schema unknown") },
			want: "this environment's migrations: schema unknown"},
		"the commit's migrations cannot be listed": {world: func(f *applyFixture) { delete(f.facts.Dirs, "repo-app@"+newSHA+":"+migrationDir) },
			want: "cannot list repo-app/" + migrationDir},
		"not a full commit id": {params: map[string]any{"commit": "main"}, want: "the request names no full commit id"},
		"frozen":               {world: func(f *applyFixture) { f.cms.Set(releasepolicy.FreezeConfigMap, frozen) }, want: "releases are frozen"},
		"expired":              {world: func(f *applyFixture) { f.clock = now.AddDate(0, 0, 91) }, want: "expired at"},
		"tier D":               {tier: releasepolicy.Tier("D"), want: "tier D is never auto-approved"},
		// A policy signed before 2026-11-15 has neither field: the action
		// always needs a card, and says so in the words every non-release gets.
		"a policy without ddl_reversible": {edit: func(doc map[string]any) {}, want: "run_agentrt_migration is not a release"},
		"ddl_reversible false": {edit: func(doc map[string]any) {
			reversible(doc)
			doc["conditions"].(map[string]any)["ddl_reversible"] = false
		}, want: "run_agentrt_migration is not a release"},
	} {
		t.Run(name, func(t *testing.T) {
			edit := c.edit
			if edit == nil {
				edit = reversible
			}
			f := newApplyFixture(t, edit)
			migrationWorld(f, inClassSQL)
			if c.world != nil {
				c.world(f)
			}
			params, tier := c.params, c.tier
			if params == nil {
				params = map[string]any{"commit": newSHA}
			}
			if tier == "" {
				tier = releasepolicy.Tier("C")
			}
			d := f.eng.Decide(context.Background(), "run_agentrt_migration", tier, params)
			if d.Auto || !strings.Contains(strings.Join(d.Reasons, "\n"), c.want) {
				t.Fatalf("auto = %v, reasons = %q, want %q", d.Auto, d.Reasons, c.want)
			}
			if strings.Contains(name, "without ddl_reversible") && d.PolicyID != "" {
				t.Fatalf("a policy that judges no migration named itself: %q", d.PolicyID)
			}
		})
	}
}

// Owner 2026-11-15: a migration that cannot destroy rows reaches PROD without
// a card; the platform runs it (cards #493 #554 #558 were Owner-run Jobs).
// A policy signed before the condition existed answers a release exactly as
// before: a migration file is a no_ddl hit.
func TestOldPolicyStillRefusesAMigration(t *testing.T) {
	f := newApplyFixture(t, func(doc map[string]any) { doc["conditions"].(map[string]any)["additive_ddl"] = false })
	migrationWorld(f, inClassSQL)
	d := f.decide(nil)
	if d.Auto || !strings.Contains(strings.Join(d.Reasons, "\n"), "hits no_ddl: "+secondFile) {
		t.Fatalf("auto = %v, reasons = %q", d.Auto, d.Reasons)
	}
}
