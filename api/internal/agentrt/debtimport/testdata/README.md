# Fixture provenance

All Markdown files contain real excerpts, with unrelated fields/paragraphs
omitted. Complete source ledgers are supplied only through environment variables.

| Fixture | Source | Coverage |
|---|---|---|
| legacy.md | TECH_DEBT at de152c7, TD-91 | §7.1 rules 1, 2, 4, 6, 7, 9, 12 |
| fields.md | Same, TD-268 and TD-92; TD-268's removed helper is renamed `PrimarySQLHelper`, because the repository-wide ratchet TestRemovedPrimarySQLHelperStaysGone forbids its real name anywhere | Rule 3, late title/free heading, plain acceptance, partial PASS |
| modern.md | Same, TD-475 | Chinese fields, free evidence and shortened paths, inferred repo |
| states.md | Same, TD-85, TD-95, TD-448, TD-451 | Rule 5, dated/undated watches and unmapped states |
| approvals.md | Same, original approval rows including TD-142 and TD-473 | Three row spellings, conditional and apply gates |
| signoffs.md | Same, TD-392, TD-384, TD-378 | Rule 9, standalone 无后续 lines |
| sections.md | Same, named overview sections and four member entries | Rules 8, 10, 11, 12 |
| misplaced.md | Same heading spellings, deliberately combined outside 条目 | Rule 1 rejection |
| ratchets.md | RATCHETS at fa7e2e2, four rows and section excerpts | Rule 13; historical/open refs, missing columns, malformed code span |
| annotated-approvals.md | TECH_DEBT at fa7e2e2, TD-293/337/393/391 | Independent clause parsing, cost/risk normalization |
| evidence-regressions.md | Same, TD-441/130/266/288/443 | Commit evidence and addresses versus file locations |
| orphan-prose.md | Same, TD-445 heading and renumbering paragraph | Unfielded prose must enter debt_text and report |
| field-keys.md | Same, TD-152 and TD-472 | Every section 3 English/Chinese key keeps its kind; area case (S5) |
| locations.md | Same, TD-183 evidence and TD-467 证据 | Workspace path:line and host:port versus file locations |
| fa7e2e2-counts.json | Source hashes and state/priority counts | Full-file source distributions; no ownership inference |

Project and repository suggestions are not independent source counts. Every
inferred repository segment and project is reported for coordinator review.
Synthetic grammar and adversarial inputs live inline in tests, not in these
real-source excerpts.
