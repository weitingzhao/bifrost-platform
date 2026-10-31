# S5 exact TypeScript policies

`manifest.json` is trusted core material, not a candidate-supplied permission file.
It enrolls only the four paths in design v3.1 §2.2. The generating checkout is
recorded separately from the design review commit: the intervening six Console
changes concern mission landing and GoalCard; the four candidates, their direct
consumers, DTO and protected tests are unchanged.

The manifest records the §3.1 FilePolicy structures, complete exported signatures
and recursive data shapes, the resolved standard-library call targets and forms
per file, six environment sites, required guard predicates, consequences and
routes, every production value/type reference, the exact selector origins and
forwarding/invocation sites, and six CSS asset edges. Test references are excluded
from production permissions; the original tests are separately bound by complete
source SHA-256.

Every recorded member is checked on each run; the manifest carries no descriptive
field that the checker does not execute. Builtin argument contracts are the §3.4
rules coded in `ts/analyze.cjs` (a profile cannot widen them), so a call rule
records only identity and form. Consumers of an environment site are the exact
`callers` contract of its enclosing function. The data-contract inventory
(sources, transfer edges, construction witnesses and consumers) is re-derived
and compared whole. `reviewedCommit`/`enrolledAtCommit` are provenance only.

The compiler host accepts only supplied tree files and declarations from the
checker's fixed dependency installation. It never reads candidate `node_modules`
or executes candidate scripts. TypeScript is exactly 5.9.3. Every library,
declaration and package metadata file actually read is SHA-256 bound in
`toolchain`; `@bifrost/ui` must come from the deployment's pinned UI revision,
recorded in `ui-revision.json`. It ships as all zeros (unpinned): the CI
materials command refuses it, so no checker image can be pinned until the core
lane records the UI commit these declarations were built from.
The external checker image and UI commit are additionally bound by CI.

The production consumer closure contains 31 function/component symbols and 73
references beyond the 119 direct candidate references. It includes the
`useFlexQueryLiveProbe` → `useOwnerConsole` transfer, so changing only the downstream
approval/verdict use cannot bypass the direct-caller check. This is a finite,
reviewed symbol-reference closure, not a general data-flow proof. Its upstream
inputs bind 534 local producer/DTO modules by complete source hash and 90 local
constructor/constant AST witnesses; local constructor dependencies are followed
with a visited set. Changes to those input contracts require core review.

All hashes use sorted-object-key canonical JSON; arrays preserve order. ASTs
ignore comments and source offsets, retain syntax kinds, operators, literal and
identifier values, resolved symbol identities and types. The guard route is
relative to its enclosing function and prevents a copy in dead code from meeting
the original guard obligation. Consumer AST hashes bind the complete containing
function; unrelated functions in a consumer file may change.
Selector source contracts also bind each surrounding loop input and the complete
capturing predicate plus its array receiver/call. Keeping `value(p)` unchanged
cannot authorize an inverted filter predicate or a different points source.
Type-only references bind their type subtree and the recursive export/DTO shape,
without freezing an unrelated function body merely because it uses an exported
return type. TypeScript's internal unique-symbol allocation numbers never enter
the digest: computed symbol identities use their declaration AST and source file.

Two deliberately conservative restrictions apply. M0's four source blobs must
match the reviewed baseline, so adoption of a newly landed source baseline needs
a core policy refresh. The complete production module-edge inventory is also
frozen: adding even an unrelated module edge requires core review. Neither
restriction creates an allow-list exception or widens the design's four paths.
An additional 65 TypeScript sources outside `console/src` have exact source seals
and static import inventories. Their local import graph has no path to any of the
four candidates at enrollment. Any addition, deletion or byte change in these
unprofiled packages is rejected; this does not claim cross-package semantic
support. Upstream producer module seals are also conservative: an unrelated edit
within such a module still needs a core policy refresh.

Re-enrollment is an offline maintainer operation:

```sh
node ts/enroll.cjs /path/to/reviewed/platform policies/manifest.json
```

Review the resulting policy diff and run all checker/behavior fixtures before
landing it through the protected core process. The production entrypoint never
invokes this generator. This manifest does not authorize a landing or demonstrate
an independent review; the server/CI must supply their separately authenticated
review, tree, generation and execution evidence.
