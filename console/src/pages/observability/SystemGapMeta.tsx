import type { GapSummary, SignalGap } from '@/lib/observability'
import { gapPartClass } from '@/pages/observability/observabilityFormat'

/** Colored system-level gap meta: fail · blind · by-design · ok */
export function SystemGapMeta({ summary }: { summary: GapSummary }) {
  const parts: { gap: SignalGap; text: string }[] = []
  if (summary.fail > 0) parts.push({ gap: 'fail', text: `${summary.fail} fail` })
  if (summary.blind > 0) parts.push({ gap: 'blind', text: `${summary.blind} blind` })
  if (summary.byDesign > 0) parts.push({ gap: 'by_design', text: `${summary.byDesign} by-design` })
  if (summary.ok > 0) parts.push({ gap: 'ok', text: `${summary.ok} ok` })
  if (parts.length === 0) return <span>—</span>
  return (
    <span
      className="font-mono-tabular"
      title={`${summary.ok} ok · ${summary.fail} fail · ${summary.blind} blind · ${summary.byDesign} by-design · ${summary.total} required`}
    >
      {parts.map((p, i) => (
        <span key={p.text}>
          {i > 0 ? ' · ' : null}
          <span className={gapPartClass(p.gap)}>{p.text}</span>
        </span>
      ))}
    </span>
  )
}
