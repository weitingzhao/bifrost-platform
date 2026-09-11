# Massive Overview — the data contract behind the reference screen

`massive-overview.html` is the first page drawn in this system. Every block on it is
answered by a read the Massive plugin already computes; nothing is judged on the
page. Numbers are from 2026-09-11 17:32 UTC (`massive-overview-sample.json`).

## Verdict strip

| Element | Answered by |
|---|---|
| Lamp + `Held` / `1 decision` tags | freshness verdicts (all ok) + open "Decide" items from the Doctor |
| The sentence | freshness ok on all 13 session-judged feeds; the Doctor's criticals trace to one listed cause |
| Meta | clean count (datasets with no partial/thin axis: 12/19) · queue state · next scheduled fires |

## The four questions

| # | Question | Read | Verdict owner | States it uses |
|---|---|---|---|---|
| 1 | Does the store hold what the session should? | `GET /market/coverage/dimensions` → `datasets[].freshness` (newest, cadence, deadline) · 5 ms cached | plugin freshness axis, judged against `data.session` | Held · T+1 · Cadence · Filings · Reference · See 3 |
| 2 | Are we holding what we pay for? | same read → `datasets[].verdicts` (76 = 19 datasets × 4 axes) + `memory.changes` | plugin four-axis verdicts | ok · boundary · partial · thin |
| 3 | What needs a decision or an action? | `GET /market/doctor` findings + dimensions (35 s live today → **must be cached** for this page) | the Doctor | Decide · Explained · Auto-fix · Release · Wait |
| 4 | Is Research getting what it needs? | `GET /market/coverage/inventory` → `analytics` · 5 ms cached; Research `orchestration/status` | plugin inventory; Research batch status | Ready · Success |

## What left the page

| Was on the Overview | Now |
|---|---|
| Data husbandry strip (Market batch, IB Flex, Research OLAP) | Massive's own lane only; IB Flex on its own page; Research as question 4 |
| Stock summary cards (computed in the browser) | folded into question 1, judged by the plugin |
| Workers & freshness (21/21), API reachability (4/4) | Ingest tab |

## The target read

One snapshot, `GET /market/overview` (not built yet), composed server-side from the
caches above so the page renders complete and correct in under a second. Until it
exists, a design may assume these four reads and must render `…` for any of them not
yet answered.

## What happened to the "Decide" item

It was real: Research's option-universe refresh had promoted 548 names to `resident`,
which the plugin snapshots whole (≈187,000 → 716,787 contracts a session). It was
resolved the same evening (Research 0.102.0, resident 22 / core 534 / edge 29) and the
Doctor returned to 0 critical. The screen keeps it because it is the best example of
what question 3 is for.
