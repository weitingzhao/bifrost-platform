import type { OperateQueueItem, OperateQueueResponse } from '@/api/operateQueueTypes'
import { buildHandoffAgentPrompt, deriveAssessmentLabel, effectiveOperateLane } from './handoff'

const legacyItem = {
  id: 'legacy',
  program_id: 'p',
  lane: 'release',
  title: 'Legacy',
  status: 'open',
  created_at: '2026-01-01T00:00:00Z',
} satisfies OperateQueueItem

const structuredItem = {
  ...legacyItem,
  id: 'structured',
  source_lane_id: 'delivery',
  operate_lane: 'governance',
  handoff_kind: 'one_off',
  risk_level: 'medium',
  acceptance_criteria: ['accepted'],
  verification_steps: ['verified'],
} satisfies OperateQueueItem

const queue = {
  open: [structuredItem],
  recent_closed: [],
} satisfies OperateQueueResponse

void effectiveOperateLane(legacyItem)
void buildHandoffAgentPrompt(structuredItem)
void deriveAssessmentLabel('not_assessed', queue, 'p')
