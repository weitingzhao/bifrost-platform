# Ops Console design system — Claude Design sync inputs

This directory is the **Ops** design system for Claude Design. It is deliberately
separate from the Trade one (`bifrost-ui/.design-sync/`, Claude Design project
`72619b7a…`): Trade is an options-trading cockpit, Ops is infrastructure operations,
and the two already disagree on what a colour means — Trade's loss red
(`--color-loss → --color-danger = #dc2626`) is the same hex as Ops' failure lamp
(`--color-lamp-red`). The Ops tokens in `project/styles.css` are named by severity.

## Layout

`project/` mirrors the Claude Design project one-to-one and is uploaded as-is:

| Path | What |
|---|---|
| `project/README.md` | the conventions — the first thing the design agent reads |
| `project/styles.css` | `--ops-*` tokens + the component styles the cards use |
| `project/guidelines/` | state vocabulary · the data contract on a design |
| `project/components/<group>/<Name>/<Name>.html` | static cards of the Console's own composition components; line 1 is the `@dsCard` marker |
| `project/reference/` | the Massive Overview reference screen, its data contract and the live readings behind it |

This is a **hand-authored** project, not a package build: the Console's composition
components (`OpsVerdictStrip`, `OpsSection`, `DashCard`, `ScoreRing`, `Meter`) live in
`console/src/components/`, are not exported as a library, and so cannot go through the
bifrost-ui converter. Each card is a faithful static rendering of the running
component. When one changes in code, change its card in the same commit.

## Syncing

With the `DesignSync` tool, into the project recorded in `config.json`:
`list_files` → `finalize_plan` (writes `README.md`, `styles.css`, `guidelines/**`,
`components/**`, `reference/**`; `localDir` = this `project/` directory) →
`write_files` with `localPath` for each file. Incremental: push only what changed.
