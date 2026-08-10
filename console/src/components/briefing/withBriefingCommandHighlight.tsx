import type { ReactNode } from 'react'
import { BRIEFING_CMD, BriefingCommandChip } from '@/components/briefing/BriefingCommandChip'

/** Split plain text on `/briefing` and wrap matches in {@link BriefingCommandChip}. */
export function withBriefingCommandHighlight(text: string): ReactNode {
  if (!text.includes(BRIEFING_CMD)) return text
  const parts = text.split(BRIEFING_CMD)
  const out: ReactNode[] = []
  for (let i = 0; i < parts.length; i++) {
    if (parts[i] !== '') out.push(parts[i])
    if (i < parts.length - 1) {
      out.push(<BriefingCommandChip key={`briefing-cmd-${i}`} />)
    }
  }
  return <>{out}</>
}
