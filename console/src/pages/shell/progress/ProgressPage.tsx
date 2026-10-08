import { CodeHealthPage } from '@/pages/CodeHealthPage'
import { CommitLineagePage } from '@/pages/CommitLineagePage'

/** Progress: commit lineage as-is, plus the code-health card. */
export function ProgressPage() {
  return (
    <div className="flex w-full min-w-0 flex-col gap-6">
      <section aria-label="Commit lineage" className="flex w-full min-w-0 flex-col">
        <CommitLineagePage />
      </section>
      <section aria-label="Code health" className="flex w-full min-w-0 flex-col">
        <CodeHealthPage />
      </section>
    </div>
  )
}
