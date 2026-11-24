# Pipeline report adapter

pipelineReportOptions(name) reuses the raw ['delivery','runs',name] cache contract
of Releases. It fetches only that pipeline, with AbortSignal; it loads no logs,
other pipelines or workflow actions. Existing consumers keep their own fetch
options until their owners migrate; sharing a key does not prove all have migrated.
pipelineReport(data, receivedAt) supplies a portable ObservationReport: cluster /
namespace / run identity, original revision/status/reason/times, source generation
time and reachability. No deployment success or relation across rebased SHAs is
inferred. Backend run retention limits remain explicit; missing expiry is unknown.

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
