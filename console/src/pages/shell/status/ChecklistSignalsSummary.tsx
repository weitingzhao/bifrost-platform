import { useQuery } from '@tanstack/react-query'
import { fetchChecklistSignals } from '@/api/checklist'
import { checklistItemLabel } from '@/lib/shell/shellStatusLine'

/** Status page summary of GET /checklist/signals. Labels come from the checklist catalog. */
export function ChecklistSignalsSummary() {
  const q = useQuery({
    queryKey: ['status', 'checklist-signals'],
    queryFn: fetchChecklistSignals,
    refetchInterval: 60_000,
    retry: false,
  })

  if (q.isLoading) {
    return (
      <section aria-label="Checklist signals" className="flex flex-col gap-1">
        <h2 className="m-0 text-sm font-medium text-foreground">Checklist</h2>
        <p className="m-0 text-sm text-muted-foreground">Loading checklist signals…</p>
      </section>
    )
  }

  if (q.isError) {
    const message = q.error instanceof Error ? q.error.message : 'Checklist signals request failed.'
    return (
      <section aria-label="Checklist signals" className="flex flex-col gap-1">
        <h2 className="m-0 text-sm font-medium text-foreground">Checklist</h2>
        <p className="m-0 text-sm text-muted-foreground">{message}</p>
      </section>
    )
  }

  const signals = q.data?.signals ?? []
  const notOk = signals.filter(row => row.signal !== 'ok')
  const streak = q.data?.quiet_success_streak

  return (
    <section aria-label="Checklist signals" className="flex flex-col gap-1">
      <h2 className="m-0 text-sm font-medium text-foreground">Checklist</h2>
      <p className="m-0 text-sm text-muted-foreground">
        {signals.length} signal{signals.length === 1 ? '' : 's'}
        {notOk.length > 0 ? ` · ${notOk.length} not ok` : ' · all ok'}
        {streak != null ? ` · quiet streak ${streak}` : ''}
      </p>
      {notOk.length > 0 && (
        <ul className="m-0 list-disc pl-4 text-sm text-muted-foreground">
          {notOk.slice(0, 5).map(row => (
            <li key={row.item_id}>
              {checklistItemLabel(row.item_id)} · {row.signal}
              {row.detail != null && row.detail !== '' ? ` · ${row.detail}` : ''}
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
