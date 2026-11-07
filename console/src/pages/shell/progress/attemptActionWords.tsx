import type { AttemptAction } from '@/api/agentRuntimeBus'

/** Silence longer than this may mean a runtime attempt is stuck. */
export const ATTEMPT_ACTION_STUCK_MS = 10 * 60_000

export function ActionLabel({ action }: { action: AttemptAction }) {
  const label = action.kind === 'result' ? `Exit ${action.code ?? '?'}` : ({ read: 'Read', edit: 'Edit', run: 'Run' }[action.kind] ?? action.kind)
  return <span className="shrink-0 font-semibold" style={action.kind === 'result' && action.code !== undefined && action.code !== 0 ? { color: 'var(--color-danger)' } : undefined}>{label}</span>
}
// Shared formatting lives with the action label; this module has no local state.
// eslint-disable-next-line react-refresh/only-export-components
export function ago(at: string | number | undefined, now: number): string {
  const ms = typeof at === 'number' ? at : Date.parse(at ?? '')
  if (!Number.isFinite(ms)) return 'time unknown'
  const s = Math.max(0, Math.floor((now - ms) / 1000))
  if (s < 60) return 'just now'
  if (s < 3600) return `${Math.floor(s / 60)} min ago`
  if (s < 48 * 3600) return `${Math.floor(s / 3600)} h ago`
  return `${Math.floor(s / 86_400)} days ago`
}
