import { CodeHealthPage } from '@/pages/CodeHealthPage'
import { CommitLineagePage } from '@/pages/CommitLineagePage'
import { AgentWorkBoard } from '@/pages/shell/progress/AgentWorkBoard'

/** Progress: the published agent-work board, then commit lineage and code health. */
export function ProgressPage() {
  return (
    <div className="flex w-full min-w-0 flex-col gap-6 overflow-x-hidden">
      <section aria-label="Agent work" className="flex w-full min-w-0 max-w-full flex-col overflow-x-hidden">
        <AgentWorkBoard />
      </section>
      <section aria-label="Commit lineage" className="flex w-full min-w-0 flex-col">
        <CommitLineagePage />
      </section>
      <section aria-label="Code health" className="flex w-full min-w-0 flex-col">
        <CodeHealthPage />
      </section>
    </div>
  )
}
