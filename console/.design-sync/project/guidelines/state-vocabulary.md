# State vocabulary

The vocabulary is closed: a design uses these words or asks for a new one. Every
word is a state **the service computed**; the page never derives one from raw
numbers. Tag colours follow `README.md` §2 — severity only.

## It holds / it ran — ok

| Word | When |
|---|---|
| **Held** | the dataset holds the session it should |
| **Session OK** | a feed ran for the current session (judged against the service's session, never the calendar date) |
| **Ready** | a downstream product has every input it needs |
| **Success** | a run / job / pipeline finished successfully |
| **Healthy** | a lane or service passes all its checks |

## Not a fault — boundary (slate)

| Word | When |
|---|---|
| **T+1** | the vendor publishes the next morning; today's absence is on schedule (ratios, treasury yields) |
| **Cadence** | the feed has its own calendar (short interest settles twice a month) |
| **Filings** | quarterly filings; a 40-day lag is normal |
| **Reference** | reference data refreshed on its own schedule (tickers, holidays, contracts) |
| **Boundary** | the subscription plan's edge — history older than the plan allows, names the vendor has no data for |
| **Due HH:MM** | the next scheduled run has not come yet |

## Not judged yet / cannot be judged

| Word | Style | When |
|---|---|---|
| **…** / **Pending n** | grey, no fill | the read has not answered yet — never a finding |
| **Unknown** | grey dashed | the read failed or a source is unavailable — say which in the tooltip |

## Needs attention

| Word | Colour | When |
|---|---|---|
| **Thin** | yellow | partly there: some names or days short of the declared target |
| **Degraded** | yellow | a service answers but a check fails |
| **See n** | yellow | this row is explained by item *n* in "Needs you" — do not double-report |
| **Missing** | red | the service says it should hold it now, and it does not |
| **Failed** | red | a run or job failed |
| **Stale** | red | older than the freshness the service declares for it |

## Action kinds — the "Needs you" list

| Word | Stripe | Means |
|---|---|---|
| **Decide** | red | only a person can choose; show the options as buttons and the stake in numbers |
| **Explained** | yellow | a red elsewhere is caused by something already listed; say by what |
| **Auto-fix** | sky | the service can repair it; one button |
| **Release** | slate | fixed in code, waiting on a deploy |
| **Wait** | slate | resolves by itself at a known time; say when |

## Words that are not allowed

`OK` for a thing that was never checked · `0` for a count that never arrived ·
`Missing` for a feed whose deadline has not passed · `Error` without the cause ·
any colour carrying direction (up/down) or money (profit/loss).
