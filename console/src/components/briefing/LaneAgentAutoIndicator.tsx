import { Sparkles } from 'lucide-react'

/** Quiet when count is 0 — no program / no pending verify_cmd phases. */
export function LaneAgentAutoIndicator({ count }: { count: number }) {
  if (count <= 0) return null
  const label = `${count} auto`
  return (
    <span
      className="inline-flex shrink-0 items-center gap-0.5 text-dense-caption text-muted-foreground"
      title={`${count} phases auto-ready`}
      aria-label={`${count} phases auto-ready`}
    >
      <Sparkles className="h-3 w-3" strokeWidth={2} aria-hidden />
      <span className="font-mono tabular-nums">{label}</span>
    </span>
  )
}
