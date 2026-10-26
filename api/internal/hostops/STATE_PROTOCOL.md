# Host operations state protocol v1 — S1 candidate

This is the storage contract for S2 (platform/watchdog) and S3 (executor/helper).
The Go package computes and verifies transitions; it does **not** install a
state store, sign production artifacts, grant a runtime capability or execute
commands. A successful reference decision is not a write permit.

## Owners and locations

The Linux root is `/var/lib/bifrost-host-ops`; the macOS root is
`/Library/Application Support/Bifrost/HostOps`. All ancestors are root-owned
and not writable by an Agent, its groups or ACLs. Provisioning is an Owner
installation action, never an automatic repair by this package.

| Directory | Sole non-root writer | Durable contents |
| --- | --- | --- |
| `platform/` (dedicated persistent volume) | dedicated repair-chain service UID | full baseline/restriction/tombstone set, artifact marks, policy head/version, freeze epoch, authorization/nonce bindings, quota reservations, lease/fencing, resource/unknown locks, audit and outbox |
| `executor/` on `.50` | `bifrost-host-exec` | local stop, policy/epoch/fencing high waters, nonce/quota, intent/step journal, before snapshots, receipts, audit chain, retryable result outbox |
| `helper/` on target | root via fixed helper only | identity generation, high waters, nonce/execution/step bindings, lifecycle journal, resource locks, before/rollback/result records |
| `watchdog/` on `.52` | `bifrost-host-watch` | independent heartbeat observations, witnessed chain heads/high waters, alert dedupe and outbox; no node or ticket keys |
| `trust/` at each verifier | root via Owner installer only | signed inventory, manifests, complete policy baselines, anchor and installed artifact marks |

Private state directories are 0700, files 0600. Only the designated service
and root may read them. The executor's stop-only local entry point is root
owned. SSH's restricted target account cannot directly access helper state.
Agent request/freeze permissions do not grant filesystem access. No same-UID,
debugger, backup API, shared group or ACL access is permitted. API results are
redacted projections; PG/Redis projections never become authorization truth.

## Atomic transaction envelope

Each store must bind transactions to `environment`, writer identity, schema,
recovery generation, monotonically increasing sequence and previous chain
head. Each record includes its canonical payload hash and resulting head.
The private store owns these values, not the caller. Read transactions must
return a coherent snapshot of policy, history, quotas, freeze, locks and
generation. `Fresh=true` is an independently verified observation, not a
caller field or a substitute for recovery checks.

Every mutation uses a cross-process exclusive lock and a compare-and-swap of
the previous sequence/head/generation. Policy mutations additionally compare
the full `Policy.Head()` and the selected `RuleDigests()` entry. A concurrent
loser reloads and revalidates; it cannot merge by dropping a restriction.

The commit primitive must:

1. Reject symlinks in every component, hardlinks/shared writable files,
   unexpected owners/modes and inode substitution; use held directory/file
   descriptors. Validate the installed root before opening state.
2. Atomically write the entire transaction to a private WAL or staging file;
   fsync the WAL/file, replace the snapshot atomically if applicable, fsync
   the parent directory, then verify resulting identity/mode/hash.
3. Return a committed receipt only after all required fsync operations. A
   callback, queue enqueue or successful `rename` alone is not durability.
4. Persist result, lock and audit changes through the same discipline. A
   crash, short write, full disk, failed fsync or lost receipt stops subsequent
   writes. If any side effect might have started, record/retain `unknown`.

Never persist a "healthy" empty default after a read/parse error. Missing
state starts frozen and enters recovery. Trust artifact installs atomically
store exact signed bytes, metadata validity and `ArtifactMark` together;
marks are keyed by namespace/environment/artifact ID. `CheckArtifact` is used
only after anchored signature verification. Zero marks are allowed only in
the independent initial installation, not as a missing-ledger fallback.
An expired higher version must not cause fallback to an older valid version.

## Side-effect ordering

| Stage | Must commit before proceeding |
| --- | --- |
| platform authorization | validated immutable plan/hash/params hash, execution/approval/nonce binding, all stable quota reservations, dependency-closure locks, audit event/outbox |
| `.50` before ticket | independent complete policy/H validation, local stop/epoch/version/fencing checks, same nonce/plan/execution binding, quota, execution intent |
| helper before each command | independent signature/boundary/prestate check, same nonce binding, step intent and sequence, fencing generation, resource locks and fresh online permit |
| completion | local step result and before/after evidence, then retryable result delivery; platform result cannot replace helper durability |

`BindNonce` returning `false,nil` means **lookup only**, including the same
plan's first-step retry. It never authorizes starting again. Future steps in
the same plan require ordered step IDs, dedupe and a fresh permit, not a new
nonce consumption. Nonce AND execution IDs are unique within environment
and recovery generation. Failure/unknown reservations remain consumed.
Counters aggregate by stable quota IDs plus host/action history, using UTC
day and ISO week. Renewal, clause rename, output deletion and restart never
reset history. Repeated-success counts and durable circuits are environment/host/action based;
rule/clause names, versions, executors and service aliases are not circuit keys.

### Repeated-success latch and scoped release (Work 592, 2026-10-26)

`SafetyState.Circuits` is required: an empty array is a verified initial ledger,
whereas missing/null state requires recovery. Each target record retains its
monotonic trip number, open flag, evidence digest and cleared execution-ID
tombstones. It is authorization state, never output subject to collection.

For every successful result, each verifier runs `Policy.ObserveSuccesses` for
the executed request under the policy/state/history CAS. Commit the result,
all resulting latches, audit and notification outbox together before result
acknowledgement or any subsequent admission. Observe while frozen as well.
A policy change must reconcile all affected targets against retained history
under the same CAS before becoming visible. A rename/version change cannot
remove a target's latch or tombstones. `Allow` also computes this transition;
a rejection with `Decision.Circuit` must durably latch through the same
transaction. A denial is not permission to discard the transition.

The reference model checks every historical sliding window since explicitly
cleared execution IDs, not only the window ending now. Retain all uncleared
success evidence (or a separately proven equivalent durable summary) while
needed to establish a trip. No output cleanup, time passage or restart closes
a circuit. Existing open latches still reject if result output was compacted.
Loss or uncertain rollback of this ledger/history freezes admission; never
substitute empty arrays for a failed read.

Only `Trust.ReleaseCircuit` with an anchored signature in the separate
`bifrost-host-ops-circuit-release` namespace proposes closing one circuit.
The signed object binds environment, host/action, exact current circuit hash
(including trip), full history hash, freeze epoch, recovery generation, policy
head/version, reason and validity. It requires healthy local time, a coherent
fresh history, durable audit and target lock; reserved/running/unknown target
executions prohibit release even with a signature. S2/S3 must prove target
quiescence including remote effects, not merely the global scheduling slot.

Apply release under the same history/state CAS and persist the signed receipt
and audit before exposing it. Retain the trip number and union of exact
success execution IDs acknowledged by the signature; do not erase quota,
history, locks or freeze state. No timestamp cutoff: a late success absent
from the signed history still counts, even if its timestamp is earlier.
Subsequent successes can trip again with a higher trip number; old releases
fail. Freeze/unfreeze, policy renewal and recovery do not release circuits.
Recovery carries circuits/tombstones forward and requires an independent
scoped release afterwards. S2/S3 must fault-test result-plus-latch atomicity,
release CAS races, restart/old-backup detection and notification durability.

Freeze increments durable epoch under CAS; any authenticated operator can
stop writes. Local stop and platform freeze are ORed. Only an anchored
`bifrost-host-ops-unfreeze` signature for the current epoch, generation, head,
minimum version and reason can clear them. Replayed unfreezes fail. A thaw
uses the same structural integrity check as admission (without the stop flags);
it cannot repair a zero version high water, mismatched epoch or missing circuit
ledger. Its signature must match the current recovery generation. A thaw
is not recovery of missing or untrustworthy state. Retain maximum epoch,
policy version, fencing and identity/recovery generation independently of
output records. Each new step needs an online permit for the current epoch
and generation, valid for at most five seconds. Revocation unavailable means
no new permit. Already started commands use their defined safe stopping
point; the exact previously authorized local rollback may finish.

## Unknown, locks and retention

`global_writes=1` limits schedulable slots, not the count of possibly still
running remote processes. Unknown executions retain target and all signed
dependency-closure isolation locks (shared storage, network, repair store,
release dependencies). Only a proven unrelated action may reuse the global
slot. If dependency scope cannot be proven, keep the original slot as well.
An expired lease, ten-minute UI grace or restored SSH is not termination
evidence. Resolve requires the later S3 Owner-signed evidence protocol; it
does not rerun the original command or invent a safe result.

Acknowledged output may be collected after the 30-day capacity policy;
audit is archived for at least 180 days. Neither policy applies to live
authorization state. `Consumption.CanCollect` requires all of:

- approval and all tickets expired plus five-second clock tolerance;
- execution terminal, rollback complete, no unknown;
- result and audit durably archived.

The UI's 500 closed-record cap never evicts nonce/step dedupe state. Open or
unknown journals are retained. Terminal journals may compact into durable
execution/plan/nonce/step-result tombstones. A collected expired authorization
still fails current expiry/generation checks. Policy restriction tombstones,
high waters and quota history survive output cleanup; only an explicitly
signed full-set replacement can retire a restriction chain.

## Startup and recovery

Startup begins frozen. Validate local integrity against live peer/witness
chain heads, sequence, epoch, policy and fencing/identity generations. A
stale backup, truncated chain, missing ledger or inability to independently
establish freshness enters isolated recovery. A periodic witness cannot
prove no consumption was lost after its checkpoint: the uncertain interval
remains unknown with locks held.

Owner's independent maintenance entry must reconcile open operations,
isolate old processes and install a signed higher recovery generation and
minimum policy version. Old plans, tickets and permits fail generation
checks. Compromised identity/keys require rebuilding trust and a separate
unfreeze. A recovery signature alone never clears uncertain side effects or
their locks. Simultaneous rollback of all copies without a fresh independent
witness cannot be detected reliably; remain stopped for Owner recovery.

S2/S3 must provide permission-denial, crash/fsync-boundary, replay after
cleanup, old-backup and restored-connection evidence. S1 unit/reference
tests do not satisfy those runtime admission gates. S4 remains blocked until
the actual storage implementations pass them.

## Mandatory local clock gate and runtime admission evidence

`ClockObservation.Check` is mandatory in `VerifyApproval` via `PlanContext.Clock`,
`Unfreeze`, `Permit.Check`, circuit release/observation and M admission. No
signature or valid wall-clock interval substitutes for this observation.
Each of platform, executor and helper obtains its own protected NTP observation:
successful sync no older than five minutes, absolute offset at most five seconds,
no rollback or future sync time. Unknown, stale or unhealthy observations deny
new authorization, thaw/release and every new step. S2/S3 must wire these entry
points without an alternate bypass and test clock failures with valid Owner
signatures; lease/watchdog durations additionally require monotonic clocks.

The three role labels in S1 tests prove shared semantics only. S2/S3/S4 must
retain independent evidence for actual file/parent-directory owners, modes,
ACLs, held descriptors and inode substitution; received image OCI digest;
forward AND rollback effects reconstructed from actual bytes; all three
private ledgers and recovery; Agent access denial; isolated signer anti-borrow
and display-A/sign-B rejection; and an installer that rejects test identities
in production trust directories. None is delivered by this library or by
`Fresh=true`. Unimplemented/unadmitted templates remain unsupported. No S4
production signature until all applicable evidence is reviewed.
