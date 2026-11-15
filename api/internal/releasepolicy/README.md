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
