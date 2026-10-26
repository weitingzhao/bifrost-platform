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
reset history. Repeated-success counts are host/action based.

Freeze increments durable epoch under CAS; any authenticated operator can
stop writes. Local stop and platform freeze are ORed. Only an anchored
`bifrost-host-ops-unfreeze` signature for the current epoch, generation, head,
minimum version and reason can clear them. Replayed unfreezes fail. A thaw
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
