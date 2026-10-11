import { agentText } from '@/pages/shell/progress/progressSummaryModel'

/** One document string, cleaned and bounded, outside any Console sentence. */
export function AgentText({ value }: { value: string | undefined | null }) {
  const text = agentText(value ?? undefined)
  if (text === '') return null
  return (
    <span data-agent-text="" className="break-words">
      {text}
    </span>
  )
}
