import type { GapSummary, SignalGap } from '@/domains/foundation/observability'
import { GAP_WORD, gapPartClass, gapSummaryTitle } from '@/domains/foundation/observability/components/observabilityFormat'

/** Colored system-level gap meta in the seven words: fail · unknown · boundary · ok */
export function SystemGapMeta({ summary }: { summary: GapSummary }) {
  const parts: { gap: SignalGap; text: string }[] = []
  if (summary.fail > 0) parts.push({ gap: 'fail', text: `${summary.fail} ${GAP_WORD.fail}` })
  if (summary.blind > 0) parts.push({ gap: 'blind', text: `${summary.blind} ${GAP_WORD.blind}` })
  if (summary.byDesign > 0) parts.push({ gap: 'by_design', text: `${summary.byDesign} ${GAP_WORD.by_design}` })
  if (summary.ok > 0) parts.push({ gap: 'ok', text: `${summary.ok} ${GAP_WORD.ok}` })
  if (parts.length === 0) return <span>—</span>
  return (
    <span
      className="font-mono-tabular"
      title={gapSummaryTitle(summary)}
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
