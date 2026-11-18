import { useQuery } from '@tanstack/react-query'
import { fetchOpenRegressions } from '@/api/regressions'

/** What main of this project is known to have broken and nobody has fixed yet. Not read is not none. */
export function OpenRegressions({ repo }: { repo: string }) {
  const read = useQuery({
    queryKey: ['ci', 'open-regressions', repo],
    queryFn: ({ signal }) => fetchOpenRegressions(repo, signal),
    refetchInterval: 60_000,
  })
  if (read.isPending) return null
  if (read.data == null) return <p role="alert" className="m-0 text-xs text-[var(--destructive)]">Known regressions on main could not be read</p>
  if (read.data.length === 0) return <p className="m-0 text-xs text-[var(--muted-foreground)]" data-testid="ci-regressions-none">No known regressions open on main</p>
  return (
    <section aria-label="Known regressions on main" className="min-w-0 rounded-xl border border-[var(--destructive)] px-3 py-2">
      <h3 className="m-0 mb-1 text-sm font-semibold">{`Known regressions on main: ${read.data.length}`}</h3>
      <ul className="m-0 flex list-none flex-col gap-1 p-0 text-xs">
        {read.data.map(r => (
          <li key={r.introduced} className="min-w-0 [overflow-wrap:anywhere]" data-testid={`ci-regression-${r.introduced.slice(0, 8)}`}>
            <code>{r.introduced.slice(0, 8)}</code>{` ${r.impact}`}
          </li>
        ))}
      </ul>
    </section>
  )
}
