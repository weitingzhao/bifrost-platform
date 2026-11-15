import '@/api/agentRuntime'
import '@/lib/architecture/opsToolRackCatalog'
import '@/lib/shell/consoleRoutes'

/**
 * The AI assessment of a card or an approval (TD-391, the Owner 2026-10-20): advice beside the runtime
 * rule's recommendation, never an answer. The runtime keeps it on the item's own stream,
 * `assess:card:<question id>` or `assess:approval:<number>` (api/internal/agentrt/store/assess.go), read
 * through GET /api/v1/agentrt/events?stream=: first the Owner request, then the ask (its refs), then the assessment (its reasons,
 * each ref resolved to the record it names) or why there is none. A stream with no events is an item
 * that was never asked (one older than assessments, or a platform that gives none).
 */
export type AssessmentKind = 'card' | 'approval'

export type AssessmentRef = { ref: string; kind: string; label: string; href: string | null; external: boolean }
export type AssessmentReason = { text: string; evidence: AssessmentRef[] }
export type Confidence = 'high' | 'medium' | 'low'

export type Assessment =
  | { state: 'none' }
  | { state: 'requested'; requestedAt: string }
  | { state: 'pending'; askedAt: string }
  | { state: 'unavailable'; reason: string }
  | { state: 'ready'; recommended: string; confidence: Confidence; reasons: AssessmentReason[]; unchecked: string[]; by: string }

/** Snapshot carried by the ordinary approval/question read, never persisted in its material. */
export type AssessmentSnapshot = { events?: unknown; error?: string }
