# Frozen ledger import

`Parse` transforms UTF-8 Markdown and explicit import context into a review
request. It performs no I/O, runs no acceptance commands and writes no database.
Reuse `Options.ImportedAt`, the ownership catalog and history answers for
byte-identical request/report JSON.

From the API module:

```sh
go run ./cmd/debtimport parse TECH_DEBT.md RATCHETS.md \
  --infra infra-checkout --repos workspace-checkouts \
  --reserved TD-490,TD-491 --at 2026-11-02T18:00:00Z \
  > request.json 2> report.json
go run ./cmd/debtimport export request.json --file ledger > ledger-export.md
go run ./cmd/debtimport export request.json --file ratchets > ratchets-export.md
```

`--reserved` is the coordinator's explicit inventory, not a guess from prose;
use `none` only after checking that no numbers are reserved. Omitting it reports
`incomplete_floor`. `--repos` defaults to the parent of `--infra`. Every sibling
Git checkout and infra itself is checked for local main and origin/main Work
trailers; all checked refs, including those with no TD trailer, are enumerated
in `floor_sources`. Missing main or shallow history is an error. The floor is
max(488, all historical infra ledger headings, all checked main Work trailers,
reserved IDs). Body references and unmerged branch trailers do not raise it.
Historical headings include deleted entries on all branches and ledger files
at earlier paths. No number is allocated by parsing.

`filed_at` has a narrower historical limit: `FirstSeen` searches only
`agent-config/TECH_DEBT.md`, across all branches, for the exact heading. It does
not follow a ledger at earlier paths. Without `--infra`, or if no date is found,
`filed_at` is null with a finding; the baseline floor is explicitly incomplete.
The package itself only knows the injected `History` and `FloorSources` values.
The CLI's default import instant is its start time; use `--at` for reproducibility.

`--projects catalog.json` replaces the import-only ownership snapshot. It is
not a signed rule. `owns` supports path globs, `**` and braces. `repo_roots`
qualifies shortened evidence paths. All suggested projects and every inferred
repository segment are listed as **inferred, pending crew review**. Neither the
snapshot counts nor the former Python audit constitute independent project/repo
verification. Only source state/priority counts are independently checked.

Requests use the design's table/column names. Text/part/decision IDs are local
handles for the eventual store to remap. `project_notes`, `last_seen`,
`umbrellas`, `source_text`, `floor_sources` and `markdown` are transport metadata,
not new database tables. Umbrellas need real IDs allocated by the store.
Unknown state/cost/risk/project fields and empty ratchet strength/scope cells
are omitted, never emitted as empty enums or strings; each unmapped state stays
in the report. Unresolved required fields still need review before a database
write.

Rule 3 accepts bold keys and only the unbolded `- 验收:` / `- 验收：` spelling.
Rule 9 separately recognizes the closed `- 验收结果:` / full-width-colon form,
retains it as `import_raw`, and reports each occurrence; it does not widen rule
3 to arbitrary bullets or timestamp-like keys. Approval clauses are independent:
annotations on cost/risk remain raw and cannot erase other mapped clauses.
An existing decision group supplies the directional start reference, avoiding
a duplicate approval-direction decision. A stale group keeps the start
references of members still listed (rule 10) until crew drops it or asks again;
its finding says so. All 13 waves receive a membership finding, including
waves whose members have all closed.

Imported runs retain source hashes; date-only results use Chicago midnight.
Undated results use the explicit import instant with a finding. Leading verdicts
win over historical sub-verdicts. Short hashes with hex letters are retained in
`at_commits.unassigned` when repository association is unknown. No CI success
or authorization is inferred. Watch deadlines use 2026 Chicago 23:59.

Evidence prose stays in `debt_text(measured)`; measurement quotes exceeding 400
characters are omitted with a finding. Code locations require a path or a file
extension and exclude dotted numeric addresses and host names with a port
(a `svc` label or a DNS suffix such as `.local`, `.lan`, `.internal`). A
leading-slash workspace path (`/stocks/<repo>/…:n`) keeps its line, maps to
the repository segment it names, and otherwise becomes a `doc_ref` with a
`workspace_path_outside_repos` finding; it never falls back to the debt's own
repository. Unresolved repositories become `doc_ref`. Commits are also
separate evidence records.

## Field-only export and source accounting

Every export fragment is either layout or pointers to records. Layout is
whitespace, Markdown delimiters and whole words from a closed set of template
labels (`状态`, `审批`, `代价`, `风险`, `repos`, `验收结果`, `验收`, `防线`);
a label glued to other text is prose. There are **no original bytes or value
baselines in the layout**. Each bound field is rendered on every call, even if unchanged.
Changing a field changes the output or produces an explicit representation error.

Prose not consumed by a recognized field is stored explicitly: inside a debt it
becomes `debt_text(import_raw)`; document-level prose becomes
`source_text(text_kind=import_raw)`. Leading and trailing layout is cut off
first, and the remaining prose belongs to the item where it starts. This
includes the TD-445 renumbering note and the four approval annotations.
Every `import_raw`, `note` and original `heading` is listed with its record path
and full value under `raw_import_raw`, `raw_note` or `raw_heading` in the report.
These records are intentional source retention, never a hidden exporter cache.

State labels/deadlines, approval gates, normalized risk and repository lists are
rendered from their fields. Canonical rendering may change the source spelling.
Each difference is reported individually as `rendered_difference`, with document,
byte range, line number, record paths, original and rendered text. State context
and approval annotations remain available in raw records. A normalized export is
a review view, not a replacement import source: reparsing it cannot recover
original historical prose and context. Keep the original frozen sources.

Tests account for **every source byte/line**, resolving it to layout, fields,
listed raw records or a source-located difference. They compare the export with
the source plus only the report's explicit exceptions. No report is supplied to
`Export`. Because the parser reports its own differences, the full-file test
pins them per bound column (fa7e2e2: state 237, apply_gate 200, risk 24), checks
each keeps its meaning, pins the text/evidence kind distributions and report
categories, and counts every section 3 key in the source independently of the
parser's key table. Structured assertions check costs, text kinds, area case,
dates, commits, locations and references. Run the nine deliberately lossy
parsers in disposable copies (with both environment variables set, the three
value-level ones must also fail `TestFullLedgers`):

```sh
python3 internal/agentrt/debtimport/mutation_check.py
```

The script first verifies a clean baseline, then requires each named assertion
to fail; compiler errors do not count as killed mutations. Production files are
never modified. Full acceptance reads `DEBTIMPORT_LEDGER` and
`DEBTIMPORT_RATCHETS`; fixtures contain short excerpts only. The hash/count
fixture checks state and priority, not inferred ownership. The CLI defaults to
both exports separated by `<!-- RATCHETS.md -->`; `--file` selects one.

Database events, idempotent persistence, visibility projection and STG prefix
mapping remain the store integration slice's responsibility. This exporter needs
import layout metadata and does not implement a general runtime database view.
