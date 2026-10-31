# S5 trusted fixtures and suites

This directory is protected core material. Candidate changes cannot edit it or
change which assertions run. `baseline/` holds byte-for-byte copies of the four
real candidate files, their direct production consumers, the QueueHistoryPoint
DTO, and the four existing behavior test files. `baseline-sha256.json` binds all
23 copies. The enclosing policy additionally binds the full source tree's
reference inventory and reviewed construction dependencies.

Copies longer than the code-health oversized threshold (800 lines) are stored
as `<original name>.fixture` (currently `MarketDataIngestTab.tsx` and
`ResearchEnginePage.tsx`) so the repository scan does not count fixtures as
source. `ts/check.test.cjs` loads them back at their original paths with
unchanged bytes and asserts the loaded hashes against `baseline-sha256.json`
(keyed by original path). Files named in the policy's `trustedSuite` keep
their plain names, since the manifest binds those paths.

`786/` preserves the three complete counterexamples from the v3.1 review.
`cases.cjs` applies mutations to complete baseline files. It retains the exact
22 named 785/786 contracts (7 positive, 15 negative), four non-comment P1–P4
changes, equivalent type-only import and comment controls, and individual
negative cases for additional imports, provenance, prototype access, borrowing,
clock/environment sites, guards, callbacks, coercion and built-in arguments.
Cross-directory MCP/scripts consumers and dynamic module expressions are included.
Every negative asserts its designated stable refusal code; unrelated TypeScript
diagnostics do not satisfy the test.
Parser regressions cover incomplete candidate, caller and DTO declarations, plus
body-less ambient functions and omitted constructor argument lists. Exact-profile
regex, undefined numeric arguments, selector predicate and loop-input mutations
also have explicit refusal codes; the two selector source checks have isolated
rule-disable sensitivity controls.

Run the actual checker tests from `ts/` with `npm test`. The tests read the
checkout's complete repository TS/TSX/JSON tree and overlay the protected snapshots;
they do not execute candidate code. The tree input and source fingerprints are
validated by the production `check()` entry. A changed fixture or missing caller
is a failure rather than an implicit skip.

## Independent behavior process

```
node testdata/behavior.cjs --root /read-only/result-tree
node --test testdata/behavior.test.cjs
```

The runner requires TypeScript **5.9.3** from its trusted tool installation. It
reads only the four exact candidate files from the supplied result tree, refuses
symlink files and files larger than 256 KiB, and executes the fixed original test
copies with a small assertion adapter. The adapter implements only the matchers
actually used by those files; an unknown matcher or import fails. The unrelated
`massiveNav` section of the C2 test file is deliberately excluded, as required by
§2.2; its three tests are not claimed as C2 coverage. No package.json, Makefile,
Vitest config, candidate test file or candidate `passed.json` is loaded.

Each timezone run includes the original **30 tests** and **122 assertions** in
total after adding default-clock Monday/Tuesday/weekend boundaries, invalid
cron/age inputs, loading/unreachable/unknown text, queue filtering/empty/null/gap
behavior and both registered selectors. The two fixed environments are `UTC`
and `America/Chicago`, with a 15-second child-process timeout and VM string/Wasm
code generation disabled. C1 explicit-now calls make zero clock reads; C2's
registered default performs exactly one read. The existing local parse and
locale sites retain their environment dependence.

The suite runs in three roles of the same file. The parent only compiles the
exact candidate files and the trusted tests. A separate `--exec` process (Node
`--disallow-code-generation-from-strings`) is the only process that runs candidate
code: each module, its original test and its added boundary block share one VM
context, so every argument the candidate receives is built inside that context and
no host-realm object (and so no host `Function`) is reachable from it. Before candidate code
loads, every intrinsic reachable from that context's global object (plus the
iterator, generator, async and async-generator chains) is frozen and every
existing global binding is made read-only; the clock counter and instant live
only in the harness closure, and `Date` is a counting wrapper whose prototype
chain and instances' `constructor` never lead to the callable original;
records are null-prototype objects written with a captured `defineProperty`, so
a prototype setter, `toJSON` or replaced builtin cannot rewrite them. It reports
only serialized call records (function, arguments, result; plain data and Dates).
A separate `--verify` process never loads candidate code: it re-runs the same
tests and boundaries against the replayed results, requires every argument and the
complete call sequence to equal its own, and alone counts tests and assertions.
A forged executor report therefore cannot supply counts, and altered, missing or
extra records fail (`behavior.test.cjs`, including the review's forge payload).

The VM is a resource/accidental-evaluation safeguard, **not a security boundary**.
Production must run this process inside the independent network-disabled,
credential-free, resource-limited container from design §6.2. The trusted parent
records process exit status and binds the checked result tree and suite digest;
stdout counts alone are not ContentEvidence and cannot grant landing. This
runner neither requests network isolation nor claims to have tested the actual
CI container. `behavior.test.cjs` exercises P1–P4, independent rejection of guard
mutations, missing-input failure, ignored candidate result-file spoofing, the
host-realm forge payload and forged or altered executor records.

Lifecycle, review/provider bindings, CI identity, quota/main intent and legacy
floor regressions belong to the other S5 lanes. These static and behavior tests
do not claim those integration scenarios passed.
