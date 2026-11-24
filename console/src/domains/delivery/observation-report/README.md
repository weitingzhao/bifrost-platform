# Pipeline report adapter

A pipeline's retained runs are read from the engine (`engineReportOptions({kind:'pipeline', pipeline})`, engineRun.ts; /engine/v1/report?kind=pipeline), the projection moved there (bifrost-engine src/report.ts pipelineReport). 2026-11-24 STG check: 24 run lists equal to the direct read. The Console's own projection (index.ts) is gone; revert that commit to bring it back.

## Exact run and independently requested logs

`run.ts` consumes existing `fetchCITimings(pipeline, run, signal)` and raw
`['ci-timing-run', pipeline, run]`; the consumer submits a specific run before
mounting it. Source identity must exactly match (zero rows is missing; multiple
or different runs fail). The host is the current Platform connection and namespace
is cicd, as implemented by the existing timings endpoint. That endpoint supplies
no cluster id; do not merge these identities across hosts. Task identities include
parent run and TaskRun name; duplicate/missing names fail rather than collide.
Results, input revision, output commit, statuses, timestamps, empty results and
step exit code zero are preserved. No dependency edges or status judgment.

A run's log tail is read from the engine (`engineReportOptions({kind:'logs', run})`, engineRun.ts; /engine/v1/report?kind=logs), the projection moved there (bifrost-engine src/report.ts runLogReport). 2026-11-24 STG check: three runs' text and last log time equal to the direct read; only each read's generated_at differs. The Console's own log projection is gone; revert that commit to bring it back.

`console/fixtures/observation-access/Reports.tsx` exposes Single run as a usable
opt-in consumer. Changing the submitted target remounts local selection/log opt-in;
it does not solve identity switching. Unknown expiry remains unknown. Production
CI pages retain their existing owner and are not migrated here. Rollback removes
the optional consumer and run adapter; no backend state or permissions change.
