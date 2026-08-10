import { memo } from 'react'
import { cn } from '@bifrost/ui'
import { sessionLogLineTone } from '@/components/agent/sessionLogTone'

export const SessionLogLine = memo(function SessionLogLine({
  line,
  index,
}: {
  line: string
  index: number
}) {
  const tone = sessionLogLineTone(line)
  return (
    <div
      className={cn(
        'console-dock-sessions__log-line',
        tone != null && `console-dock-sessions__log-line--${tone}`,
      )}
      data-i={index}
    >
      {line}
    </div>
  )
})
