import {
  componentLineById,
  trackTypeById,
  type ComponentLineId,
  type WorkTrackType,
} from '@/lib/briefing/briefingViewTabs'
import { laneById, type LaneId } from '@/lib/briefing/workLanes'

/** Shared Init Mode framing — empty Ready lane + New Lane registration. */
function initModeHeader(title: string, lines: string[]): string {
  return [
    `## ${title}`,
    '',
    ...lines,
    '',
    '### First-reply protocol (`/briefing` five sections)',
    '1. **Echo** Session Title + Content (this lane label/description) verbatim.',
    '2. **Understanding** — what Init Mode means here (propose initial queue, not implement yet).',
    '3. **Sources** — system facts (empty queue, lane catalog) vs guidance (this Init Mode section).',
    '4. **Status** — Plan/discovery (Ready / empty queue) until Owner confirms the proposed queue.',
    '5. **Next directions** — propose 3–5 initial queue items; invite Owner to adjust or confirm, then execute.',
    'After Owner confirms, use existing MCP tools (e.g. `report_phase_progress`) to write back progress — do not invent new endpoints.',
  ].join('\n')
}

/**
 * Session pack section when the selected lane queue is empty (Ready / empty).
 * Replaces a bare "(empty queue)" stub with actionable Init Mode instructions.
 */
export function formatEmptyLaneInitSection(laneId: LaneId): string {
  const lane = laneById(laneId)
  return initModeHeader(`Lane Init Mode — ${lane.label} (${lane.id})`, [
    `**Lane:** ${lane.label} (\`${lane.id}\`)`,
    `**Description:** ${lane.description}`,
    '',
    '### Status',
    'This lane has an **empty queue** (Ready). No queued work items yet.',
    '',
    '### Your task (Init)',
    'Propose **3–5 initial queue items** for this lane:',
    '- Short label + one-sentence scope',
    '- Primary repo / files when known',
    '- Concrete and Owner-actionable (not vague themes)',
  ])
}

/**
 * Clipboard pack after registering a lane via POST /api/v1/lanes.
 * No longer instructs editing workLanes.ts — catalog is YAML-backed.
 */
export function buildNewLaneInitPack(
  line: ComponentLineId,
  tt: WorkTrackType,
  description: string,
  laneId?: string,
): string {
  const lineDef = componentLineById(line)
  const ttDef = trackTypeById(tt)
  const idLine =
    laneId != null && laneId !== ''
      ? `**Lane ID:** \`${laneId}\` (registered via API → \`config/lanes.yaml\`)`
      : '**Lane ID:** _(register via Briefing New Lane form → POST /api/v1/lanes)_'
  return initModeHeader('New Lane Init Pack', [
    `**Component Line:** ${lineDef.label} (\`${line}\`)`,
    `**Track Type:** ${ttDef.label} (\`${tt}\`)`,
    idLine,
    `**Description:** ${description.trim()}`,
    '',
    '### Your task (Lane registered)',
    'This lane is registered in `config/lanes.yaml` (single write path via platform-api).',
    '',
    '1. Propose **3–5 initial queue items** for the new lane',
    '2. Optionally add icon mapping in `briefingIcons.tsx` if a distinct glyph helps',
    '3. Do **not** hardcode lane entities in `workLanes.ts` — projection only',
  ])
}

/** Whether a lane queue should use Init Mode in the session pack. */
export function isEmptyLaneInit(queue: { length: number } | undefined | null): boolean {
  return queue == null || queue.length === 0
}

/** Default spine track for a component line when creating a lane. */
export function defaultTrackForLine(line: ComponentLineId): string {
  switch (line) {
    case 'rocket':
      return 'build'
    case 'satellite':
      return 'migrate'
    case 'engineer':
      return 'automate'
    case 'ground':
      return 'infra'
    case 'operations':
      return 'operate'
    case 'subcontractor':
      return 'automate'
  }
}

export function slugLaneId(label: string): string {
  return label
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 48)
}
