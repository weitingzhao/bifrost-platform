# Research Runtime Ignition — Operate Queue follow-ons (P6 handoff)

Canonical items now live in `research-runtime-ignition.yaml` → `post_completion.operate_queue_items`.
This file is a human-readable mirror.

## 1. research-radar-news-source

| Field | Value |
|-------|--------|
| operate_lane | business-advisory |
| handoff_kind | one_off |
| risk_level | medium |
| title | Research Event Radar — choose news ingest source |
| reason | Runtime Ignition left Event Radar news ingest out of scope; FE EmptyState points here. |

**Decision needed**: Research-workspace input directory mount vs Plugin RSS agent vs manual API.

**Acceptance**
- Owner decides news source architecture
- Ingest path documented
- Event Radar empty state retires when first batch lands

**Verify**
- Operate Queue item visible after Owner approves post-completion
- `GET /research/event-radar/events` returns rows after ingest enabled

## 2. plugin-options-tape

| Field | Value |
|-------|--------|
| operate_lane | governance |
| handoff_kind | one_off |
| risk_level | medium |
| title | Market Data Plugin — market.option_trades for Order Sentiment |
| reason | Order Sentiment uses `option_snapshot_aggregates` proxy; FE shows snapshot proxy badge. |

**Acceptance**
- Plugin writes `market.option_trades`
- New `research.order_sentiment_daily` rows use non-proxy `data_source`
- Trade FE proxy badge retires

**Verify**
- Confirm Plugin schema + worker path
- Backfill one day and check `data_source`

**Blocker (2026-08-21):** Operate Queue `plugin-options-tape` is blocked on Polygon `/v3/trades` HTTP **403** entitlement. Cron/worker path is deployed; live ConfigMap keeps `option-trades.max_per_underlying=2` (git aligned) until Options Trades entitlement is confirmed, then raise fanout.

## Owner next step

1. **Engineer → In Flight** → lane **Data layer (PG + Redis)** (`data-layer-k3s`) — should reappear after `post_completion` was restored (gates already signed).
2. Complete **Post-completion** (Approve) so the two follow-ons enter Operate Queue (`pending_review` → open).
3. Delivery Board remains the read-only program catalog.
