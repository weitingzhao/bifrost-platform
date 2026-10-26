# Host operations S1 candidate contract

Source of scope: Work 584, amended by Work 592 (2026-10-26); reviewed design `a8eae89`, sections 2–6.1.
This package is shared library code for platform/executor/helper builds in
the platform repository. It adds no HTTP route, scheduler, SSH client,
privileged helper, installer, production key, signer or runtime action.
No external dependency was added. The existing SSHSIG verifier is reused;
the release-policy Owner fingerprint is **not** used as a host-ops default.

## Signed objects and verification flow

| Object | SSHSIG namespace | Message |
| --- | --- | --- |
| `Inventory` | `bifrost-host-ops-inventory` | exact complete input YAML/JSON bytes |
| `Bundle` manifest | `bifrost-host-ops-bundle` | exact complete input YAML/JSON bytes |
| `PolicyBundle` (all baseline rules) | `bifrost-host-ops-policy` | exact complete input YAML/JSON bytes |
| `Unfreeze` | `bifrost-host-ops-unfreeze` | exact complete input YAML/JSON bytes |
| `CircuitRelease` | `bifrost-host-ops-circuit-release` | exact complete input YAML/JSON bytes |
| H `Plan` | `bifrost-host-ops-approval` | ASCII `sha256:<64 lowercase hex>` of canonical plan bytes, no newline |

The caller supplies a protected, independently installed trust anchor to
`Trust`. Empty/untrusted anchors, wrong domains, appended signatures, changed
bytes and expired scope fail closed. Tests create ephemeral in-memory keys;
no private keys or production policies are stored in fixtures.

Candidate generation serializes the structs (all non-omitempty fields
required, including false/zero and empty `[]`/`{}` values). Owner's future S3
trusted signer independently parses and displays the same immutable object
whose bytes/hash it signs. This library deliberately exposes no signing
endpoint. Being on main, passing CI or possessing an admin token cannot
replace the appropriate signature.

Each local verifier checks signatures, closed schema and semantic validity;
then checks environment, validity and installed version/digest. It prepares
the install/transition for the atomic transaction in `STATE_PROTOCOL.md`.
`Inventory`/`Bundle` parsing alone is not authorization; use installed marks
and the composing admission checks. Artifact validity is at most 90 days.
Dates are whole-second RFC3339 with timezone; no local-time inference.

`Decode` rejects unknown/missing fields, duplicate keys, anchors/aliases,
merge keys, explicit YAML tags, multiple documents, nulls, implicit scalar
coercion and nondecimal/fractional limits. There are no command expressions
or regex grants. Each input/complete serialized set is capped at 1 MiB.
Bundle entries are relative regular files, never absolute/dot traversal,
symlinks, hardlinks, duplicate or unlisted entries. Mode/owner/size/SHA256
must match; image purpose additionally requires an immutable OCI digest.
The receiver must obtain `ReceivedFile` observations through protected file
descriptors and securely extract archives in S3. This model does not unpack
archives or establish OS permissions by trusting caller-provided metadata.

## Complete-set policy semantics

`PolicyBundle` signs the full required rule set so a missing rule cannot be
mistaken for an intentional omission. Each unsigned `Tightening` carries
the full previous effective head, the selected rule digest and a strictly
increasing set version. `Load` verifies every link against a persisted
expected head/minimum version, owns its memory and exposes an immutable
snapshot. Signature verification uses the supplied original bytes before any
JSON snapshot encoding; malformed UTF-8 is rejected. New/changed Owner baselines must bind `replaces_digest` and all
`replaces_rules` (which include the prior restriction chain in the head).

Unsigned changes preserve baseline rule/clause IDs and domains. Matching
always uses original environment/action/hosts/executors/parameter sets;
the current values are additional predicates. Rules and clauses each
intersect. Tombstones, disabled/expired clauses and subset exclusions deny
their original domain. No coverage denies. A missing chain or baseline
denies the entire M set. Rules may not physically disappear from a chain.
Deletion is a retained tombstone. Unchanged fields are compared exactly;
only explicitly proven directions in `Tightens` can vary. This includes
rejecting changes to timeout, watchdog, rollback, repeated-success window,
inventory/catalog/handler/bundle digests, schema, environment and calendar.

Counts include reserved/running/failed/unknown/successful attempts with the
same supplied historical state. Both stable quota and host/action history
prevent resets by renaming. The v1 replacement admission conservatively
refuses conflicting quota IDs on overlapping host/action scopes; reconcile
such a candidate's stable quota mapping before signing instead of resetting
history. Signed replacements never modify history, freezes or unknown locks.
`SafetyState.Circuits` adds mandatory durable environment/host/action latches.
`ObserveSuccesses` returns a transaction proposal to persist with each result;
`Allow` returns a circuit proposal on a detected trip, never a write permit.
Threshold crossings in historical windows still latch after time passes.
Only a scoped, domain-separated `CircuitRelease` closes a current trip; exact
cleared execution-ID tombstones preserve replay and late-result safety.
See `STATE_PROTOCOL.md` for result/release CAS and mandatory retention.

`Allow` is the pure complete-policy reference model. `EvaluateM` composes it
with anchored inventory/manifests, installed marks, authenticated executor,
scope and independent hard boundaries. Neither grants execution by itself:
S2 reserves quota/locks atomically; S3 independently rechecks and durably
records intent before each permitted step. Later action adapters still
return unsupported until their implementation and admission evidence exist.

## H plan and boundaries

Protocol v1 canonicalization uses Go `encoding/json` on the closed `Plan`
struct in declared field order, sorted map keys, UTF-8 and HTML escaping,
with no whitespace or final newline. Array order is significant. Noncanonical
wire bytes fail verification. The hash binds the entire target identity,
params, before state, all forward/rollback artifact digests, inventory,
catalog, handler, approval/execution/nonce, executor, validity, epoch,
identity/recovery/fencing generations, rollback and dependency lock closure.
Every change requires a new Owner confirmation/signature. A signer must not
sign a supplied bare hash. S3 must prove signer program/IPC/UI isolation and
display-A/sign-B rejection; these are not delivered by S1.

`PlanContext`, `Facts`, `SafetyCatalog` and effect metadata must come from
each verifier's trusted adapter, never from the action request or an
"approved" flag. Forward **and** rollback effects must be reconstructed
from actual verified bytes; an opaque bundle has no admissible proof.
`VerifyApproval` checks current observations, installed capability, Owner
signature, freeze/nonce/generation, local clock health and independent boundary. Replay is
lookup-only. S2/S3 add atomic nonce/step consumption and online revocation.

`SafetyCatalog` is application-independent input from the trusted canonical
identity catalog, separately pinned by digest; a submitted inventory may not
replace its mapping. D10 vectors use the names in AGENT_FACTS §8a (`daemon`,
`bifrost-engine(.service)`, `trading_engine`). Runtime adapters must reuse
that authoritative mapping rather than infer identity from action names.
Execution enablement/scale-up/guard removal is rejected before H signature
authorization. Read/observe effects have positive controls. The original
platform D10 gates are unchanged; host routing cannot take over platform
manifest, credential or free-shell execution.

The never-M list is code, including normalized systemd instances and trusted
launchd aliases. Indirect install/container effects are checked too. Executor
self-maintenance is refused even for H. Only a proven first worker startup
has the join exception; S5 must additionally prove lifecycle/CAS/no workloads.
Stateful compose recreation requires H. Network/join/cleanup/toolchain/UGOS
templates are not opened by this candidate library.

## Evidence and handoff

Run from `api/` with caches under `~/agent-work/.cache`:

```sh
go build ./...
go vet ./...
go test ./...
go test -race ./internal/hostops
go test ./internal/hostops -run TestCompletePolicySubsetProperty -v
```

`testdata/intersection.json` and `testdata/d10.json` are common conformance
vectors for the three future consumers. Executing the library under the
three role labels proves common semantics, **not** three deployed verifiers.
Property tests cover 86,400 state combinations, transitivity, both overlap
layouts, history/outcomes, mixed restrictions, locks and freezes. Directed
tests cover numeric overflow, signed-byte/domain tampering, nonce replay,
retention, future/expired permits and immutable snapshot ownership.

Before S2, Owner reviews the candidate schema, stable asset/quota naming,
never-M/identity effect mappings, complete-set replacement semantics,
canonical H bytes and `STATE_PROTOCOL.md`. The reviewed first-M candidate
scope remains seven Linux/macOS actions: service restart/state, container
restart, stateless compose reconcile, monitoring install/remove and binary-
only agentd update. This is scope review, not production authorization.
S1's planned independent review rounds remain required. S2 uses test anchors
and synthetic events. Production M/H still requires S2/S3 runtime evidence,
Owner's isolated signer/installer, and S4 scope approval/signatures.

## Work 592 candidate API and R value constraints

This is an uninstalled S1 schema amendment, with no production compatibility
claim. Consumers must supply a non-null `SafetyState.Circuits` ledger and
`PlanContext.Clock`; `Unfreeze` and `Permit.Check` now require an explicit local
`ClockObservation`. Zero observations deny. M's existing clock fields use the
same check. Circuit release uses the trusted clock fields in `Facts`.

For `host_logs`, `lines` is a canonical unsigned decimal string from 1 through
200. `since` is a timezone-qualified RFC3339 whole-second instant no later than
the evaluation time; relative time expressions/options are unsupported.
Parameter values reject invalid UTF-8 and control characters. `service` must
be the signed object's canonical name or explicitly registered alias and
resolve through the trusted catalog to that same object. Every read effect
must resolve to the same identity and bind its registered config digest.
Catalog knowledge of a different service never grants inventory access.
Adapters still recheck actual service/config, translate values to fixed argv,
limit output and redact at the target; this package performs no log I/O.

S2/S3/S4 runtime admission requirements remain mandatory, including protected
file/parent/inode checks, actual OCI digest and forward/rollback effect
reconstruction, three independent durable stores, Agent permission denial,
signer isolation and refusal to install test identities into production trust.
A valid reference signature or three test-role labels is not this evidence.
