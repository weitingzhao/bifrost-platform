# releasepolicy

The engine that decides whether a release or an apply may start on the policy
the Owner signed, without his approval. What it cannot establish, it refuses.

## Changing how the engine judges

1. Make the change and run the tests:
   `go test ./internal/releasepolicy/...` (from `api/`).
2. `TestDecisionSnapshots` fails for every answer your change altered.
   Rewrite the golden files:
   `go test ./internal/releasepolicy -run TestDecisionSnapshots -update`
3. Read `git diff testdata/decisions`. That diff is your statement of which
   answers changed. A request that went from refused to covered without
   your meaning it to is a bug in your change. Commit the golden files in
   the same commit as the change.
4. Never touch `mustrefuse/`. It lists the requests that must be refused
   whatever else changes, and only the Owner edits it. If your change makes
   one of its cases covered, the change is wrong, not the case. A release
   whose diff touches that directory waits for the Owner (the policy's
   `owner_only` paths).

A change under this directory is a trust-anchor change. The policy covers
its release when the commit's CI succeeded (CI runs the must-refuse set and
the snapshots) and the diff touches no owner-only path.

## Migrations that cannot destroy rows

With `conditions.ddl_reversible: true` and a `ddl_reversible_paths` list in
the signed policy (Owner 2026-11-15), a migration file under those paths is
not a `no_ddl` refusal when it is new in the diff and every top-level
statement is of the class `ClassifyMigration` names (`migrationclass.go`): an
allow-list by statement kind, so what it does not recognise is out. A changed
or deleted migration file is never in the class, and such a file is never
handed to the older `additive_ddl` line check, which would pass it.

The same check covers the action `run_agentrt_migration`
(`migrationaction.go`): the platform runs its own environment's pending
migrations of a commit when the pipeline the policy pins this environment's
pipeline to delivered that commit successfully and every pending file is new
since the commit the environment runs and of the class. A policy without the
condition covers neither; both wait for the Owner as before.

A function body is opaque to the class: the statement that carries it defines
it and does not run it. What a replaced function does when something calls it
later is vouched for by the commit's CI and by the source environment having
run the same migration, not by this check.
