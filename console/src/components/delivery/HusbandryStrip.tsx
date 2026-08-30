import { useEffect } from 'react'
import { Button, DenseTag, type DenseTagVariant } from '@bifrost/ui'
import { useQuery } from '@tanstack/react-query'
import { ExternalLink } from 'lucide-react'
import { syncHusbandryChecklist } from '@/api/checklist'
import { fetchDataHusbandry, type HusbandryLaneView } from '@/api/dataHusbandry'
import { resolveOpsToolUrl } from '@/lib/architecture/opsToolRackCatalog'
import { massiveReadinessHref } from '@/lib/research/massiveNav'

const HUSBANDRY_SYNC_KEY = 'bifrost.husbandrySyncAt'
const HUSBANDRY_SYNC_TTL_MS = 60 * 60 * 1000

function verdictVariant(v: string): DenseTagVariant {
  if (v === 'healthy') return 'success'
  if (v === 'due' || v === 'draining' || v === 'caution') return 'warning'
  if (v === 'missed' || v === 'degraded') return 'danger'
  return 'neutral'
}

function LaneChip({ lane }: { lane: HusbandryLaneView }) {
  return (
    <li
      className="inline-flex items-center gap-1.5 rounded border border-border/60 bg-card px-2 py-1"
      title={lane.detail}
    >
      <DenseTag variant={verdictVariant(lane.verdict)}>{lane.verdict.toUpperCase()}</DenseTag>
      <span className="text-dense-caption font-medium text-foreground">{lane.label}</span>
      {lane.source != null && lane.source !== '' ? (
        <span className="text-dense-micro text-muted-foreground">src={lane.source}</span>
      ) : null}
    </li>
  )
}

function shouldSyncHusbandry(overall: string | undefined): boolean {
  if (overall !== 'degraded' && overall !== 'caution') return false
  try {
    const raw = sessionStorage.getItem(HUSBANDRY_SYNC_KEY)
    if (raw != null) {
      const at = Number(raw)
      if (Number.isFinite(at) && Date.now() - at < HUSBANDRY_SYNC_TTL_MS) return false
    }
  } catch {
    /* ignore */
  }
  return true
}

/** Shared Market / Flex / Research husbandry strip (void ≠ fail; Job Complete ≠ success). */
export function HusbandryStrip({ className }: { className?: string }) {
  const q = useQuery({
    queryKey: ['data-husbandry'],
    queryFn: fetchDataHusbandry,
    refetchInterval: 30_000,
    retry: 1,
  })
  const snap = q.data

  useEffect(() => {
    if (snap == null || !shouldSyncHusbandry(snap.overall)) return
    let cancelled = false
    void syncHusbandryChecklist()
      .then(() => {
        if (cancelled) return
        try {
          sessionStorage.setItem(HUSBANDRY_SYNC_KEY, String(Date.now()))
        } catch {
          /* ignore */
        }
      })
      .catch(() => {
        /* fail-soft — strip still shows lanes */
      })
    return () => {
      cancelled = true
    }
  }, [snap?.overall, snap?.detail])

  return (
    <div
      className={[
        'flex flex-col gap-1.5 rounded-md border border-border bg-secondary/20 px-2.5 py-2',
        className ?? '',
      ].join(' ')}
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="m-0 text-dense-meta font-semibold uppercase tracking-wide text-muted-foreground">
          Data husbandry
        </p>
        <div className="flex flex-wrap items-center gap-2">
          <p className="m-0 text-dense-caption text-muted-foreground">
            {q.isLoading
              ? 'Loading…'
              : snap != null
                ? `${snap.overall} · ${snap.detail}`
                : q.error != null
                  ? (q.error as Error).message
                  : '—'}
          </p>
          <Button variant="outline" size="sm" className="h-7 gap-1 px-2 text-dense-caption" asChild>
            <a
              href={massiveReadinessHref()}
              title="Open Massive Coverage → Readiness"
            >
              <ExternalLink size={12} aria-hidden />
              Open Massive
            </a>
          </Button>
          <Button variant="outline" size="sm" className="h-7 gap-1 px-2 text-dense-caption" asChild>
            <a
              href={resolveOpsToolUrl('dagster')}
              target="_blank"
              rel="noopener noreferrer"
              title="Open Dagster batch schedule UI"
            >
              <ExternalLink size={12} aria-hidden />
              Open Dagster
            </a>
          </Button>
        </div>
      </div>
      {snap != null ? (
        <ul className="m-0 flex list-none flex-wrap gap-1.5 p-0">
          {snap.lanes.map(lane => (
            <LaneChip key={lane.id} lane={lane} />
          ))}
        </ul>
      ) : null}
      {snap?.note != null ? (
        <p className="m-0 text-dense-micro text-muted-foreground/80">{snap.note}</p>
      ) : null}
    </div>
  )
}
