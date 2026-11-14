

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
