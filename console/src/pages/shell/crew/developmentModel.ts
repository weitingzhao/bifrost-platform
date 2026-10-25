import type { RuntimeProgress } from '@/api/agentRuntime'
import type { PlanRead } from '../agents/useRuntimePlan'

/** A display freshness limit, not a qualification or authorization rule. */
export const DEVELOPMENT_FRESH_MS = 120_000
export const DEVELOPMENT_REVIEW = '2026-10-24 21:15 CDT'
export const R4_HREF = '#work?section=plan&round=R4'
export const CAPABILITY_LADDER = [
  { title: 'See clearly', proof: 'Partial evidence', benefit: 'Know which work is useful and what its complete delivery costs.', gap: 'Comparable delivery, rework and human-effort records.', rounds: '4.5 · 4.7', href: '#crew?section=capacity' },
  { title: 'Deliver reliably', proof: 'Engineering collaboration demonstrated', benefit: 'One authorized goal becomes a usable result without manual handoffs.', gap: 'Automatic landing, STG identity and health, and intervention evidence.', rounds: '4.1', href: '#work?goal=M-143' },
  { title: 'Allocate well', proof: 'Outcome not yet demonstrated', benefit: 'Assignment adapts to performance and available quota while quality holds.', gap: 'Comparable results and qualified candidates across providers.', rounds: '4.5 · 4.8', href: '#crew?section=roles' },
  { title: 'Run multiple products', proof: 'Outcome not yet demonstrated', benefit: 'Independent products keep moving while the Owner is away.', gap: 'Separate queues and release lanes, off-laptop operation and recovery.', rounds: '4.2 · 4.3 · 4.4 · 4.6 · 4.9', href: R4_HREF },
  { title: 'Keep improving', proof: 'Outcome not yet demonstrated', benefit: 'Better outcomes at the same investment, even when models change.', gap: 'Repeatable comparisons, replacement exercises and governance cost.', rounds: '4.7 · 4.8 · 4.10', href: '#crew?section=capability-evidence' },
] as const
export const R4_OUTCOMES: Record<string, string> = {
  '4.1': 'An accepted change reaches main and STG without a coordinating thread writing.',
  '4.2': 'Two products make progress and deliver independently.',
  '4.3': 'Each application has its own release pace and signed policy.',
  '4.4': 'Work continues overnight with the MacBook closed.',
  '4.5': 'See what is running, where it is blocked and how quota affects assignment.',
  '4.6': 'A week of work without manual branch, workspace or disk cleanup.',
  '4.7': 'Fewer Owner decisions and less intervention, measured over a week.',
  '4.8': 'Qualification follows model changes and records re-examination.',
  '4.9': 'A failed machine has a tested recovery path and visible notification.',
  '4.10': 'Verify the five agreed Round 4 outcomes with real observations.',
}
export type DevelopmentRead = { state: 'ready'; progress: RuntimeProgress; plan: Extract<PlanRead, { from: 'runtime' }>; observedAt: number } |
  { state: 'loading' | 'error' | 'offline' | 'absent' | 'stale' | 'unknown'; reason: string; observedAt?: number }
/** A fresh HTTP answer cannot make an old generated snapshot current. Failed reads discard cached claims. */
export function developmentRead(progress: { data?: RuntimeProgress; isError: boolean; isPaused: boolean; error: unknown; dataUpdatedAt: number }, plan: PlanRead, planAt: number, now: number): DevelopmentRead {
  const observedAt = progress.dataUpdatedAt > 0 && planAt > 0 ? Math.min(progress.dataUpdatedAt, planAt) : undefined
  if (progress.isPaused) return { state: 'offline', reason: 'The runtime read is paused offline.', observedAt }
  if (progress.isError) return { state: 'error', reason: progress.error instanceof Error ? progress.error.message : 'Runtime read failed.', observedAt }
  if (plan.from === 'error') return { state: plan.error === 'offline' ? 'offline' : 'error', reason: plan.error, observedAt }
  if (!progress.data || plan.from === 'loading') return { state: 'loading', reason: 'Reading runtime progress and the Round 4 plan…', observedAt }
  if (plan.from === 'absent') return { state: 'absent', reason: 'Round 4 is not imported in this environment.', observedAt }
  const generated = Date.parse(progress.data.generated_at)
  if (!observedAt || !Number.isFinite(generated) || generated > now + DEVELOPMENT_FRESH_MS || plan.round.n !== 4) return { state: 'unknown', reason: 'The observation time or Round 4 identity could not be verified.', observedAt }
  if (now - Math.min(observedAt, generated) > DEVELOPMENT_FRESH_MS) return { state: 'stale', reason: 'The last complete reading is over two minutes old; current progress is unknown.', observedAt }
  return { state: 'ready', progress: progress.data, plan, observedAt }
}
