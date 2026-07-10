import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  PageHeader,
  SegmentControl,
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  StatusLamp,
} from '@bifrost/ui'
import { fetchMatrix, isAllMatrices } from '@/api/platform'
import type { MatrixResponse, Target } from '@/api/types'
import { OpsSection } from '@/components/layout/OpsSection'
import { consumeSatelliteApiEnv } from '@/lib/task-mode/readinessChipActions'
import { summarizeTradeReadiness } from '@/lib/control-room/matrixSummary'

const ENV_OPTIONS = [
  { value: 'dev', label: 'Dev' },
  { value: 'stg', label: 'Stg' },
  { value: 'prod', label: 'Prod' },
] as const

type MatrixEnv = (typeof ENV_OPTIONS)[number]['value']

const CATEGORY_ORDER = ['trade_frontend', 'trade_api', 'datastore', 'trade_auth', 'trade_write'] as const

function categoryLabel(category: string): string {
  switch (category) {
    case 'trade_frontend':
      return 'Frontend'
    case 'trade_api':
      return 'API'
    case 'datastore':
      return 'Datastore'
    case 'trade_auth':
      return 'Auth'
    case 'trade_write':
      return 'Write (blocked)'
    default:
      return category
  }
}

function authVariant(auth: string): 'success' | 'warning' | 'danger' | 'neutral' {
  if (auth === 'ok') return 'success'
  if (auth === 'skipped') return 'neutral'
  if (auth === 'missing' || auth === 'invalid') return 'warning'
  return 'danger'
}

function sortTargets(targets: Target[]): Target[] {
  return [...targets].sort((a, b) => {
    const ai = CATEGORY_ORDER.indexOf(a.category as (typeof CATEGORY_ORDER)[number])
    const bi = CATEGORY_ORDER.indexOf(b.category as (typeof CATEGORY_ORDER)[number])
    const aOrder = ai >= 0 ? ai : 99
    const bOrder = bi >= 0 ? bi : 99
    if (aOrder !== bOrder) return aOrder - bOrder
    return a.id.localeCompare(b.id)
  })
}

export function SatelliteApiHealthPage() {
  const focusedEnv = consumeSatelliteApiEnv()
  const [env, setEnv] = useState<MatrixEnv>(focusedEnv ?? 'prod')
  const [selected, setSelected] = useState<Target | null>(null)

  const matrixQuery = useQuery({
    queryKey: ['matrix', 'all'],
    queryFn: () => fetchMatrix(),
    refetchInterval: 30_000,
  })

  const matrices = useMemo((): MatrixResponse[] => {
    const data = matrixQuery.data
    if (data == null) return []
    if (isAllMatrices(data)) return data.matrices
    return [data]
  }, [matrixQuery.data])

  const matrix = matrices.find(m => m.environment === env)
  const targets = sortTargets(matrix?.targets ?? [])
  const apiTargets = targets.filter(t => t.category === 'trade_api')
  const readiness = summarizeTradeReadiness(targets)
  const excludedCount = targets.length - readiness.total
  const readinessOk = readiness.ok === readiness.total && readiness.total > 0

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <PageHeader
        title="API Health"
        description="Per-environment matrix probes for Trade satellite endpoints — HTTP reachability and ops auth."
      />

      <OpsSection title="Environment matrix" bodyPadding="default" overflow="visible">
        <div className="mb-3 flex flex-wrap items-center gap-3">
          <span className="text-xs font-medium text-muted-foreground shrink-0">Environment:</span>
          <SegmentControl value={env} options={[...ENV_OPTIONS]} onChange={v => setEnv(v as MatrixEnv)} />
          <DenseTag variant={readinessOk ? 'success' : 'warning'}>
            {matrixQuery.isLoading
              ? '…'
              : `${readiness.ok}/${readiness.total} readiness OK`}
          </DenseTag>
          {excludedCount > 0 && (
            <span className="text-[var(--text-dense-caption)] text-muted-foreground">
              {excludedCount} policy / skipped (not scored)
            </span>
          )}
          {matrix?.generated_at != null && (
            <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Probed {new Date(matrix.generated_at).toLocaleString()}
            </span>
          )}
        </div>

        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Target</DenseTableHead>
              <DenseTableHead>Category</DenseTableHead>
              <DenseTableHead>Reach</DenseTableHead>
              <DenseTableHead>Auth</DenseTableHead>
              <DenseTableHead>Detail</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {matrixQuery.isLoading ? (
              <DenseTableRow>
                <DenseTableCell colSpan={5} className="text-[var(--muted-foreground)]">
                  Loading matrix…
                </DenseTableCell>
              </DenseTableRow>
            ) : targets.length === 0 ? (
              <DenseTableRow>
                <DenseTableCell colSpan={5} className="text-[var(--muted-foreground)]">
                  No targets for {env}
                </DenseTableCell>
              </DenseTableRow>
            ) : (
              targets.map(target => (
                <DenseTableRow
                  key={target.id}
                  className="cursor-pointer hover:bg-[var(--secondary)]"
                  onClick={() => setSelected(target)}
                >
                  <DenseTableCell className="font-mono-tabular font-medium">{target.id}</DenseTableCell>
                  <DenseTableCell>{categoryLabel(target.category)}</DenseTableCell>
                  <DenseTableCell>
                    <StatusLamp value={target.reachability} kind="reach" />{' '}
                    <span className="font-mono-tabular">{target.reachability}</span>
                  </DenseTableCell>
                  <DenseTableCell>
                    <DenseTag variant={authVariant(target.auth)}>{target.auth}</DenseTag>
                  </DenseTableCell>
                  <DenseTableCell className="max-w-xs truncate text-[var(--muted-foreground)]">
                    {target.detail || '—'}
                  </DenseTableCell>
                </DenseTableRow>
              ))
            )}
          </DenseTableBody>
        </DenseDataTable>

        <p className="m-0 mt-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {apiTargets.length} Trade API domains · click a row for URL and authorization detail.
        </p>
      </OpsSection>

      <Sheet open={selected != null} onOpenChange={open => !open && setSelected(null)}>
        <SheetContent side="right" className="w-full max-w-md overflow-y-auto">
          {selected != null && (
            <>
              <SheetHeader>
                <SheetTitle>{selected.id}</SheetTitle>
              </SheetHeader>
              <dl className="mt-4 space-y-3 text-[var(--text-dense-meta)]">
                <div>
                  <dt className="text-[var(--muted-foreground)]">Category</dt>
                  <dd className="m-0 font-medium">{categoryLabel(selected.category)}</dd>
                </div>
                <div>
                  <dt className="text-[var(--muted-foreground)]">Reachability</dt>
                  <dd className="m-0 flex items-center gap-2">
                    <StatusLamp value={selected.reachability} kind="reach" />
                    {selected.reachability}
                  </dd>
                </div>
                <div>
                  <dt className="text-[var(--muted-foreground)]">Auth</dt>
                  <dd className="m-0">{selected.auth}</dd>
                </div>
                <div>
                  <dt className="text-[var(--muted-foreground)]">Authorization level</dt>
                  <dd className="m-0 font-mono-tabular">{selected.authorization_level || '—'}</dd>
                </div>
                <div>
                  <dt className="text-[var(--muted-foreground)]">URL</dt>
                  <dd className="m-0 break-all font-mono-tabular text-[var(--text-dense-caption)]">
                    {selected.url ?? '—'}
                  </dd>
                </div>
                <div>
                  <dt className="text-[var(--muted-foreground)]">Detail</dt>
                  <dd className="m-0">{selected.detail || '—'}</dd>
                </div>
              </dl>
            </>
          )}
        </SheetContent>
      </Sheet>
    </div>
  )
}
