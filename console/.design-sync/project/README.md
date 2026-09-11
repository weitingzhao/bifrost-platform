# Designing the Bifrost Ops Console

The **Ops Console** is the control plane of Bifrost: the screens an operator uses to
see whether the machinery that feeds a trading system is doing its job — market-data
feeds (Massive), broker statements (IB Flex), the research batch, clusters,
pipelines, releases. The reader is scanning for **what is wrong and what needs
them**, then acting.

This is **not** the Trade UI, and it does not share Trade's design language. Trade
is an options-trading cockpit where red and green mean *down* and *up*. Here a
colour means one thing only: **severity**. Never borrow a trading convention.

House style: **the verdict first, quiet chrome, loud exceptions.**

## 1. Dark only

One palette, no light theme. Tokens live in `styles.css` under `--ops-*`.

| Token | Hex | Use |
|---|---|---|
| `--ops-ground` | `#0a0c0f` | page |
| `--ops-surface` | `#13171d` | cards, sections, sidebar |
| `--ops-raised` | `#1a1f26` | rows, items inside a section |
| `--ops-line` / `--ops-line-soft` | `#2a313c` / `#20262f` | borders / inner dividers |
| `--ops-ink` / `--ops-ink-mute` / `--ops-ink-faint` | `#e4e9ef` / `#7a8492` / `#525b67` | text |
| `--ops-accent` | `#a3e635` | the **one** primary action per view, the active route — nothing else |

Give every panel you draw its own ground; near-white ink on an unpainted container
disappears against the host.

## 2. Colour is severity — and there are exactly these states

| State | Colour | Means | Never use it for |
|---|---|---|---|
| ok | green `--ops-ok` | holds / ran / healthy | "up", "profit" |
| warn | yellow `--ops-warn` | degraded, thin, needs a look | "unrealized" |
| fail | red `--ops-fail` | broken, missing, decide now | "down", "loss" |
| boundary | slate `--ops-boundary` | not a fault: the plan's edge, a feed on its own cadence, a T+1 publisher | a gap |
| unknown | grey dashed | cannot be judged: a read failed, a source is unavailable | "missing" |
| pending | grey `…` | the read has not answered yet | anything — it is not a finding |
| info | sky `--ops-info` | informational, auto-fixable | severity |

See `guidelines/state-vocabulary.md` for every tag word and when it applies.

## 3. Every page has three acts

1. **Verdict** — the verdict strip, always visible, never collapsed: a lamp, an
   uppercase title (`MASSIVE · SESSION 2026-09-10`), state tags, **one sentence
   that states the conclusion in words**, a meta line of evidence, and — if the
   verdict drives an action — the primary action top-right in the accent.
2. **Body** — sections. A section whose signal is not OK is **open**; a healthy
   one is **collapsed** (healthy detail is one click away, never on the first
   screen). Pages with three sections or fewer may keep all open.
3. **Actions** — the core actions are always discoverable: on the verdict strip or
   on the item they act on, never buried inside collapsed detail.

Page identity lives in the console header breadcrumb (`PLUGIN › Massive`), not in
an in-page title bar. Tabs sit under it (`Overview · Coverage · Ingest`).

## 4. Organise a page by the questions its owner asks — not by components

Before placing a single panel, write down the 3–5 questions the person who owns
this page needs answered, in the order they would ask them. Each becomes a
numbered section; the number is the reading order. For a **data-source plugin's
Overview** the questions are fixed — see `components/patterns/FourQuestionOverview`:

1. **Session** — does it hold what this session should?
2. **Subscription** — are we holding what we pay for?
3. **Needs you** — what needs a decision or an action?
4. **Downstream** — are consumers getting what they need?

Process signals (workers alive, API reachable) are always green and answer none of
these: they go on an Ingest/Runtime tab. A sibling system (IB Flex on Massive's
page) does not appear at all; a *consumer* (Research) appears only as question 4.

The failure this prevents: a page laid out by component, where each panel judges
health its own way, will show "missing" and "healthy" side by side about the same
data — exactly what the Massive Overview did before this system.

## 5. Data rules every design follows

- **Render the verdict; never compute it.** The service owns every judgement
  (`ok`, `thin`, `boundary`…). A design shows the service's word and its evidence.
- **Not arrived is not a finding.** A value still loading is `—` with a `…` tag,
  counted as pending. It is never drawn as Missing, thin, zero or red.
- **A bar needs a denominator the service declares.** A meter divided by its own
  count reads 100% forever. No declared scope → no bar (hatched track, "no scope").
- **Every block names its source** on the right of its header: which read answers
  it and how fast (`dimensions · 5 ms cached`). See `guidelines/data-contract.md`.
- **Say the cadence.** A feed that publishes next morning is `T+1`; quarterly
  filings are `Filings`; reference data is `Reference`. Only a feed that missed
  *its own* deadline is late.
- **Numbers line up**: monospace, tabular figures, right-aligned in columns.
- **Real numbers in every design.** Use the sample readings in `reference/` — a
  design drawn with placeholder data hides whether the page can be answered at all.

## 6. Type and density

Geist for text, JetBrains Mono for numbers and identifiers. Dense scale: 13 px
body, 12 px labels, 11 px meta, 10 px captions and uppercase section titles
(`letter-spacing: .1em`), 9 px micro. Radius 10 px on sections, 6–8 px inside.

## 7. Components

| Card | Use for |
|---|---|
| `composition/VerdictStrip` | Act 1 of every page; compact variant for a one-row status |
| `composition/Section` | Act 2 containers; collapsible, with source label |
| `status/StatusLamp` | one live reading; outline for auth/config states |
| `status/StateTag` | every state word — the vocabulary is closed |
| `status/ActionItem` | the "Needs you" list: severity stripe, cause, stake in numbers, the action |
| `data-display/DashCard` | one metric with its state and (declared-denominator) meter |
| `data-display/ScoreRing` | an n-of-m rollup; pending fills no segment |
| `data-display/AxisBar` | a distribution of verdicts across a fixed set (the four axes) |
| `patterns/FourQuestionOverview` | the Overview skeleton of any data-source plugin |

## 8. Reference screen

`reference/massive-overview.html` is the first page drawn in this system, with live
readings from 2026-09-11 17:32 UTC. `reference/massive-overview-contract.md` maps each
of its blocks to the read and the verdict that answer it; `reference/massive-overview-sample.json`
holds those readings for new designs.

## 9. Do not

- Use red/green for direction, profit or loss — there is none here.
- Hand-roll a verdict strip or a section frame; use the two composition cards.
- Put a healthy section open on the first screen, or a core action inside a collapsed one.
- Show two lamps side by side that answer different questions (one "reachable",
  one "batch adherence") — the reader will compare them.
- Draw a loading state as a verdict, or a plan boundary as a gap.
