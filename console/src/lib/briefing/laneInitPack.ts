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
    '### First-reply protocol',
    '1. Confirm understanding of this Init Mode briefing.',
    '2. Propose the items below (or adjusted list) — mark one *(recommended)*.',
    '3. **Wait for Owner confirmation** before implementing.',
    '4. After Owner confirms, use existing MCP tools (e.g. `report_phase_progress`) to write back progress — do not invent new endpoints.',
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

/** Clipboard pack for registering a brand-new lane in workLanes.ts. */
export function buildNewLaneInitPack(
  line: ComponentLineId,
  tt: WorkTrackType,
  description: string,
): string {
  const lineDef = componentLineById(line)
  const ttDef = trackTypeById(tt)
  return initModeHeader('New Lane Init Pack', [
    `**Component Line:** ${lineDef.label} (\`${line}\`)`,
    `**Track Type:** ${ttDef.label} (\`${tt}\`)`,
    `**Description:** ${description.trim()}`,
    '',
    '### Your task (Register lane)',
    'Register a new lane in the Briefing system based on the description above.',
    '',
    '1. Add a lane definition to `bifrost-platform/console/src/lib/briefing/workLanes.ts`:',
    '   - Add the new ID to the appropriate `LaneId` union type',
    `   - Create a \`WorkLane\` entry with \`componentLine: '${line}'\`, \`trackType: '${tt}'\``,
    '   - Choose appropriate `track` (spine data source) and `workIntent`',
    '2. Add an icon mapping in `bifrost-platform/console/src/lib/briefing/briefingIcons.tsx`',
    '3. Run `cd bifrost-platform/console && npx tsc --noEmit && npm run build` to verify',
  ])
}

/** Whether a lane queue should use Init Mode in the session pack. */
export function isEmptyLaneInit(queue: { length: number } | undefined | null): boolean {
  return queue == null || queue.length === 0
}
