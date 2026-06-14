import type { BriefingSnapshotInput } from '@/lib/briefing/briefingSnapshot'
import { formatBriefingLiveStatus } from '@/lib/briefing/briefingSnapshot'
import { CATALOG_SOURCE, CATALOG_VERSION } from '@/lib/environments-catalog'
import { formatUiProgressSection } from '@/lib/briefing/uiProgressSnapshot'

/** Meta-briefing: task for an Agent to audit and sync Agent Briefing with the live platform. */
export function buildBriefingAlignmentPack(input: BriefingSnapshotInput): string {
  const now = new Date().toISOString()

  const spineMilestones =
    input.context?.milestones
      .filter(m => m.status === 'IN_PROGRESS' || m.status === 'BLOCKED_ON')
      .map(m => `${m.id} (${m.status})`)
      .join(', ') ?? '(spine not loaded)'

  return [
    '# Agent Briefing — alignment task brief',
    `Generated: ${now}`,
    'Work intent: briefing_alignment (meta)',
    'Mode: Ops',
    '',
    '## Your task for this session',
    '',
    'Audit **Agent Briefing** and related LLM context generators so they match the **current** Ops Platform (Console tabs, platform-api routes, spine milestones, and docs).',
    'Produce a minimal PR that fixes drift only — do not refactor unrelated code.',
    '',
    '### Out of scope unless Owner asks',
    '- bifrost-trade-frontend pages (unless updating frontend work-intent copy)',
    '- bifrost-trade-api migration (Phase 1 discipline still applies)',
    '- bifrost-trader-engine/ (read-only)',
    '',
    '## Three-layer alignment model',
    '',
    '| Layer | Source | Briefing behavior |',
    '|-------|--------|-------------------|',
    '| Auto | GET /context, /matrix, /cluster, /observability, /health | Live status + spine/matrix packs refresh on Generate — verify accuracy, fix bugs only |',
    '| Semi-auto | agentContextPacks.ts, matrixSummary.ts, formatSpineContextSection | Changes propagate when shared helpers change — ensure Briefing still calls them |',
    '| Manual | uiProgressSnapshot.ts, workIntents.ts, buildBriefingPack readFirst/appendices | **Must update on every Console/API ship** — primary drift risk |',
    '',
    '## Files to audit (in order)',
    '',
    '1. `console/src/pages/ConsolePage.tsx` — ViewTab + navGroups (ground truth for Console tabs)',
    '2. `console/src/lib/briefing/uiProgressSnapshot.ts` — CONSOLE_UI_PROGRESS table + generated pack section',
    '3. `console/src/lib/briefing/buildBriefingPack.ts` — readFirst, avoid, appendices, session discipline',
    '4. `console/src/lib/briefing/buildBriefingAlignmentPack.ts` — this meta pack (keep checklist current)',
    '5. `console/src/lib/briefing/workIntents.ts` — work intent labels/descriptions',
    '6. `console/src/lib/control-room/agentContextPacks.ts` — Ops/Promote/Product packs used by Briefing',
    '7. `api/internal/server/server.go` — registered /api/v1/* routes',
    '8. `docs/TRADE_CONTRACT.md` + `docs/CLUSTER_ACTUATION.md` — API contract docs',
    '9. `docs/ARCHITECTURE.md` + `docs/AGENT_MODES.md` — view matrix + Briefing workflow',
    '10. `config/ops-context.yaml` — spine milestones/focus (auto in pack; confirm Program UI matches)',
    `11. ${CATALOG_SOURCE} — CATALOG_VERSION must match ops-context meta.catalog_version (make test runs check_spine_catalog.sh)`,
    '',
    '## Drift checklist',
    '',
    '- [ ] Every Console nav tab in ConsolePage has a matching CONSOLE_UI_PROGRESS row (status + notes)',
    '- [ ] New platform-api routes appear in TRADE_CONTRACT.md and uiProgress API rows if user-facing',
    '- [ ] buildBriefingPack readFirst paths exist and match intent (ops/feature/debug/release/cluster/frontend)',
    '- [ ] Cluster appendix mentions current Layer A/B endpoints (GET /cluster/metrics, /cluster/observability)',
    '- [ ] WORK_INTENT_OPTIONS still cover Owner workflows; add intent if a new flywheel dominates',
    '- [ ] AGENT_MODES.md documents Briefing + alignment pack for new sessions',
    '- [ ] After edits: `cd bifrost-platform && make test` passes',
    '',
    '## Spine milestones needing narrative sync',
    '',
    `- active_in_progress_or_blocked: ${spineMilestones}`,
    input.context?.focus.blocker
      ? `- spine_blocker: ${input.context.focus.blocker} — ensure release/ops intents mention it`
      : '- spine_blocker: (none)',
    '- milestone ops-ui-actuation: align uiProgress + ARCHITECTURE with north star P0–P4',
    '',
    '## Baseline snapshot (current session — diff against codebase after audit)',
    '',
    formatBriefingLiveStatus(input),
    '',
    formatUiProgressSection(),
    '',
    '## Related LLM context entry points (keep consistent, not duplicate)',
    '',
    '- Agent Briefing — session pack + this alignment meta pack',
    '- Control Room → Agent focus dock — scoped governance packs',
    '- Catalog → Copy for LLM — full static catalog + spine',
    '- Runtime Map → Copy all — topology + selection-scoped infra pack',
    '',
    '## Suggested opening message (paste to Agent)',
    '',
    'Mode: Ops. Work intent: briefing_alignment. Task: align Agent Briefing with the current Ops Platform. Use the drift checklist and baseline snapshot above. Read ConsolePage nav, uiProgressSnapshot.ts, buildBriefingPack.ts, buildBriefingAlignmentPack.ts, TRADE_CONTRACT.md, and ops-context.yaml. List concrete drift items, then implement the smallest diff. Run `make test` in bifrost-platform. Reply in Chinese.',
    '',
    '## Session discipline',
    '- Reply in Chinese for dialogue; English for UI strings and code identifiers.',
    '- One repo (bifrost-platform) unless drift requires doc-only edits in listed docs.',
    `- catalog_version baseline: ${CATALOG_VERSION}`,
  ].join('\n')
}
