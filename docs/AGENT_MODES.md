# Agent interaction modes — Bifrost Ops Platform

Bifrost Trade uses two flywheels (product vs runtime). Agents and Owner should declare which mode applies before making changes.

**North star**: All routine ops via Ops Console/API (Strategy C hybrid). See [NORTH_STAR.md](NORTH_STAR.md) and Program → north star in spine.

## Modes

| Mode | Flywheel | Default UI | Agent may | Agent must not |
|------|----------|------------|-----------|----------------|
| **Product** | A — Trade FE | `bifrost-trade-frontend` :5173 | Migrate pages, Dense UI, hooks, Legacy equivalence | Change compose, prod cutover, K3s, API contracts |
| **Ops** | B — Runtime | Bifrost Ops Console :5180 → **Control Room** | Read spine, matrix, topology; infra YAML; K3s planning | Change trade page UI, expand FE scope |
| **Promote** | A + B coupling | Ops → Control Room / Promote | Single-variable release checks, sign-off docs | Skip blockers (D1, gate), mix API + FE in one change |

## Control Room context packs

On **Control Room** → **Agent focus dock**, three Copy buttons build layered clipboard text (length increases Product &lt; Ops &lt; Promote). Each pack starts with `Mode: Product|Ops|Promote`.

| Button | Contents |
|--------|----------|
| **Copy Product** | Phase 1 discipline + spine focus/deployment — **no** matrix |
| **Copy Ops** | Full spine section + matrix summary per environment |
| **Copy Promote** | Ops pack + flywheel A/B checklist + promote blockers + prod fail list |
| **Copy Scoped** | (when a pipeline milestone is selected) Ops pack + scoped milestone/decision |

Suggested mode rules: `focus.blocker` or `flywheel_primary === B` → Ops; Promote bay or cutover milestone → Promote; else Product.

Catalog tab **Copy for LLM** still uses the full static catalog + spine (`buildEnvironmentsLlmContext`).

## Context pack layers

When starting a Cursor session, prefer this order:

1. **Discipline** — workspace rules, migration-protocol, dense-ui-system
2. **Spine** — `GET http://127.0.0.1:8780/api/v1/context` or Ops Console → Catalog → Copy for LLM
3. **Task scope** — one milestone id, one env (`dev` / `prod`), one repo
4. **Live probe** — only if task touches connectivity: `GET /api/v1/matrix?env=...`
5. **Deep doc** — `MIGRATION_TRACKING.md`, sign-off runbooks (on demand)

Authoritative milestone state: [config/ops-context.yaml](../config/ops-context.yaml)  
Static hardware/catalog: [console/src/lib/environments-catalog.ts](../console/src/lib/environments-catalog.ts)

## Opening prompts (examples)

- Product: `Mode: Product. Task: migrate LivePage SSE hook only. No API or infra changes.`
- Ops: `Mode: Ops. Task: verify prod matrix blockers; read spine D1. No frontend edits.`
- Promote: `Mode: Promote. Task: assess if prod cutover is allowed; list blockers from spine + matrix.`

## Forbidden (all modes)

- `daemon_control` write via platform AI
- `ib:operator:cmd` RPC
- Editing `bifrost-trader-engine/` (read-only reference)

## Related

- [ARCHITECTURE.md](ARCHITECTURE.md) — control plane vs data plane
- [TRADE_CONTRACT.md](TRADE_CONTRACT.md) — probe endpoints
- [Goal/AI_NATIVE_OPS_PLATFORM.md](../../bifrost-trade-infra/Goal/AI_NATIVE_OPS_PLATFORM.md) — north star
