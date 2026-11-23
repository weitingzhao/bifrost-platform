# Pipeline report adapter

pipelineReportOptions(name) reuses the raw ['delivery','runs',name] cache contract
of Releases. It fetches only that pipeline, with AbortSignal; it loads no logs,
other pipelines or workflow actions. Existing consumers keep their own fetch
options until their owners migrate; sharing a key does not prove all have migrated.
pipelineReport(data, receivedAt) supplies a portable ObservationReport: cluster /
namespace / run identity, original revision/status/reason/times, source generation
time and reachability. No deployment success or relation across rebased SHAs is
inferred. Backend run retention limits remain explicit; missing expiry is unknown.
