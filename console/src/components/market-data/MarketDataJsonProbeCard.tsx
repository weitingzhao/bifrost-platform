import { useCallback, useState, type ReactNode } from 'react'
import { Button } from '@bifrost/ui'
import { probeMarketPath } from '@/api/marketDataPlugin'

export function MarketDataJsonProbeCard({
  title,
  description,
  defaultPath = '/market/coverage/db-summary',
  fields,
}: {
  title: string
  description?: string
  defaultPath?: string
  fields?: ReactNode
}) {
  const [path, setPath] = useState(defaultPath)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [data, setData] = useState<Record<string, unknown> | null>(null)

  const execute = useCallback(async () => {
    setBusy(true)
    setError(null)
    setData(null)
    try {
      const res = await probeMarketPath(path.trim())
      if (!res.ok) {
        setError(res.error)
        return
      }
      setData(res.data)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }, [path])

  return (
    <div className="rounded-md border border-dashed border-[var(--border)] bg-[var(--secondary)] px-3 py-3">
      <p className="m-0 text-[var(--text-dense-label)] font-semibold">{title}</p>
      {description ? (
        <p className="m-0 mt-0.5 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {description}
        </p>
      ) : null}
      <div className="mt-3 flex flex-col gap-2">
        {fields}
        <label className="flex flex-col gap-1">
          <span className="text-[var(--text-dense-meta)] font-medium text-[var(--muted-foreground)]">
            Plugin path
          </span>
          <input
            className="h-8 rounded-md border border-[var(--border)] bg-[var(--background)] px-2 font-mono text-[var(--text-dense-meta)]"
            value={path}
            onChange={e => setPath(e.target.value)}
            placeholder="/market/coverage/db-summary"
          />
        </label>
        <div>
          <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => void execute()}>
            {busy ? 'Running…' : 'Execute'}
          </Button>
        </div>
        {error ? (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">{error}</p>
        ) : null}
        {data ? (
          <pre className="m-0 max-h-80 overflow-auto rounded-md bg-[var(--background)] p-3 text-[var(--text-dense-caption)] font-mono">
            {JSON.stringify(data, null, 2)}
          </pre>
        ) : null}
      </div>
    </div>
  )
}
