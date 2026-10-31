# S5 content checking (design v3.1, line A)

This protected package implements content evidence for the four exact Console
TypeScript paths in `policies/manifest.json`. It is not the landing authorization
predicate. Review authentication, signed rules, CI identity, generation, quota and
main intent remain responsibilities of the controller/store/landingci integration.
No Go candidate language profile exists.

## Entry points

```go
CheckS5Candidate(ctx, trustedTreeReader, trustedPolicy, CandidateKey) ContentEvidence
CheckStaticTrees(ctx, trustedTreeReader, trustedPolicy, M0, R) (StaticEvidence, error)
```

`GitTreeReader` reads immutable Git objects, both path endpoints and modes, verifies
commit/tree identities and parent/patch correspondence, and never checks out or
executes candidate files. `NodeChecker` invokes the deployment-owned `ts/check.cjs`
with one JSON request on stdin and consumes one canonical JSON response on stdout:

```text
{schema:2, baseline:{files:{path:{content,blob,mode}}},
 result:{files:{path:{content,blob,mode}}}, policies:<trusted manifest>, key:<CandidateKey>}
-> {findings, policyDigest, callersDigest, typesDigest, testsDigest}
```

The Go wrapper binds these digests to the full candidate key. `toolchainDigest`
is not reported by the checker: it is SHA-256 of the canonical JSON of the
checker image's `TOOLS.lock.json` (`ToolchainDigest`, the same definition infra
`docker/s5-checker/verify_materials.py` checks at image build), supplied as
`TrustedPolicy.ToolsLock`; the lock must also pin the manifest's TypeScript
version. Compiler version, `console/tsconfig.json` and every declaration the
compiler host reads stay bound through the profile (`manifest.typescript`,
`configs`, `toolchain` → `policyDigest`) and are re-measured on every run.
`testdata/ci-materials-golden.json` pins the cross-language values. It cannot set
`ContentEvidence.passed` until a separate `TrustedBehaviorRunner` returns a success
bound to the exact result tree and suite digest. The command's flag mode is
static inspection only: it always emits `StaticEvidence` with `passed:false`, and
exit zero means only that static checking passed. Content evidence comes only
from `--ci-request-stdin`, which runs the behavior suite in its separate process.

## CI modes (image built by infra `docker/s5-checker`)

```text
s5check --ci-materials <policyRoot> <TOOLS.lock.json>
  -> canonical {profileDigest, toolchainDigest, callersDigest, typesDigest, testsDigest, uiSHA}
s5check --ci-request-stdin
  <- canonical {schema:2, key:S5CIKey, ciUID, platformGitDir, uiGitDir, policyRoot}
  -> canonical schema-2 ContentEvidence; exit 0 only when passed
```

`--ci-materials` refuses unless the installed manifest and the four checker
scripts are byte-equal to the copies compiled into the binary, every
`trustedSuite` file matches its manifest hash, the lock pins the enrolled
TypeScript version and `policies/ui-revision.json` pins a non-zero UI commit.
The shipped UI pin is all zeros, so an image cannot be built (and S5 stays
closed) until the core lane pins the UI revision the manifest's `@bifrost/ui`
declarations were enrolled from.

`--ci-request-stdin` decodes one canonical request (unknown, duplicate and
case-variant members refused; every member must be present, landing-valid key, Kubernetes UID, absolute paths), recomputes the
installation's materials from `<policyRoot>/..` (`TOOLS.lock.json`,
`node_modules`) and requires the key to carry exactly them, requires
`refs/s5/{base,a,m0,revision}` and `refs/s5/ui` in the read-only bare stores
to name the key's commits, then runs `CheckS5Candidate` with the protected
`ts/check.cjs` and `NodeBehaviorRunner`. The runner writes only the four
enrolled blobs of R into a private temporary directory and runs
`testdata/behavior.cjs` in a separate Node process; that runner executes candidate
code only in its own executor process and counts in a verifier process that never
loads candidate code. Completion is the trusted harness report (both timezones,
30 tests / 122 assertions each) plus exit status, bound to R's tree and the manifest's tests digest. This process never
loads candidate source, a candidate `node_modules`, script or result file.

## Trusted installation

Use Node 22 or later and exactly TypeScript 5.9.3 (locked in `ts/package-lock.json`).
The compiler host reads standard libraries and declaration files from the fixed
checker dependency installation, never from candidate `node_modules`. The fixed
installation must also contain the reviewed Console declaration dependencies and
`@bifrost/ui` built from the UI SHA bound by CI. All files actually read are hashed
against the manifest. Installing only TypeScript suffices for isolated AST unit
probes, but does not satisfy the complete production caller/type contract.

From `api/`, a static inspection is:

```sh
go run ./cmd/s5check \
  --repo /absolute/read-only/platform \
  --baseline FULL_M0_SHA --result FULL_R_SHA \
  --policies /trusted/s5check/policies/manifest.json \
  --checker /trusted/s5check/ts/check.cjs \
  --checker-sha256 DEPLOYMENT_ENTRYPOINT_SHA256 \
  --node-module-path /trusted/node_modules
```

All paths and digests above are deployment inputs. Do not compile the checker,
regenerate policies, install dependencies or obtain behavior commands from R.
The TypeScript process performs static analysis only. The independent
`testdata/behavior.cjs --root /read-only/result` runner executes the fixed original
assertions and extra boundaries in UTC and America/Chicago; its local VM is not a
replacement for CI container isolation.

## Verification and limits

Run `npm test`, `npm run lint`, and `npm run build` in `ts/` with the fixed
dependencies available; run `go build ./...`, `go vet ./...`, and `go test ./...`
in `api/`. The Node suite includes the original 22 contract probes, full-file
P1–P4 mutations, concrete refusal-code assertions, static-rule sensitivity mutants
that exist only in test memory, and independently executed behavior checks.

See `policies/README.md` for deliberately conservative baseline, module-edge and
producer restrictions. The finite interpreter rejects changing nested source
labels across loops, treats closure captures as deeply read-only, and allows only
scalar writes to fresh object fields. It does not prove loop termination or
business correctness. Behavior tests and authenticated semantic review are still
mandatory.
