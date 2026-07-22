/**
 * Satellite → API & Auth Probes (tab id: satellite-api).
 *
 * Probe-detail page for Trade satellite matrix targets — HTTP reachability,
 * ops auth, and D10 blocked write paths. System health verdict lives in
 * Mission Control → Observability; this page must not surface a readiness badge.
 */

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
import { fetchMatrix, isAllMatrices } from '@/api/core'
import type { MatrixResponse, Target } from '@/api/matrixTypes'
import { OpsSection } from '@/components/layout/OpsSection'
import { consumeSatelliteApiEnv } from '@/lib/task-mode/readinessChipActions'

const ENV_OPTIONS = [
  { value: 'dev', label: 'Dev' },
  { value: 'stg', label: 'Stg' },
  { value: 'prod', label: 'Prod' },
] as const

type MatrixEnv = (typeof ENV_OPTIONS)[number]['value']

/** Main probe table — write paths are listed in a dedicated D10 section. */
const CATEGORY_ORDER = ['trade_frontend', 'trade_api', 'datastore', 'trade_auth'] as const

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
  // missing / invalid / blocked → danger (auth issues are this page's unique value)
  return 'danger'
}

function sortProbeTargets(targets: Target[]): Target[] {
  return [...targets].sort((a, b) => {
    const ai = CATEGORY_ORDER.indexOf(a.category as (typeof CATEGORY_ORDER)[number])
    const bi = CATEGORY_ORDER.indexOf(b.category as (typeof CATEGORY_ORDER)[number])
    const aOrder = ai >= 0 ? ai : 99
    const bOrder = bi >= 0 ? bi : 99
    if (aOrder !== bOrder) return aOrder - bOrder
    return a.id.localeCompare(b.id)
  })
}

function AuthCell({ target }: { target: Target }) {
  const level = target.authorization_level?.trim()
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <DenseTag variant={authVariant(target.auth)}>{target.auth}</DenseTag>
      {level != null && level !== '' && (
        <span className="font-mono-tabular text-[var(--text-dense-caption)] text-muted-foreground">
          {level}
        </span>
      )}
    </div>
  )
}

export function SatelliteApiHealthPage({
  onOpenObservability,
}: {
  onOpenObservability?: () => void
} = {}) {
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
  const allTargets = matrix?.targets ?? []
  const writeTargets = useMemo(
    () =>
      [...allTargets.filter(t => t.category === 'trade_write')].sort((a, b) =>
        a.id.localeCompare(b.id),
      ),
    [allTargets],
  )
  const probeTargets = useMemo(
    () => sortProbeTargets(allTargets.filter(t => t.category !== 'trade_write')),
    [allTargets],
  )
  const apiTargets = probeTargets.filter(t => t.category === 'trade_api')

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <PageHeader
        title="API & Auth Probes"
        description="Per-environment matrix probes for Trade satellite endpoints — HTTP reachability, ops auth, and D10 blocked write paths. Health verdict → Mission Control → Observability."
      />

      <OpsSection title="Endpoint probes" bodyPadding="default" overflow="visible">
        <div className="mb-3 flex flex-wrap items-center gap-3">
          <span className="text-xs font-medium text-muted-foreground shrink-0">Environment:</span>
          <SegmentControl value={env} options={[...ENV_OPTIONS]} onChange={v => setEnv(v as MatrixEnv)} />
          {matrix?.generated_at != null && (
            <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Probed {new Date(matrix.generated_at).toLocaleString()}
            </span>
          )}
          {onOpenObservability != null && (
            <button
              type="button"
              className="focus-strip-link text-[var(--text-dense-caption)] ml-auto"
              onClick={onOpenObservability}
            >
              View Observability
            </button>
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
            ) : probeTargets.length === 0 ? (
              <DenseTableRow>
                <DenseTableCell colSpan={5} className="text-[var(--muted-foreground)]">
                  No probe targets for {env}
                </DenseTableCell>
              </DenseTableRow>
            ) : (
              probeTargets.map(target => (
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
                    <AuthCell target={target} />
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

      <OpsSection
        variant="flat"
        title="Write paths blocked (D10)"
        bodyPadding="default"
        overflow="visible"
        description="Platform L0 must not probe live write paths (trade-execution-freeze / R-DV3). Auth=blocked is intentional governance evidence, not a reachability failure."
      >
        {writeTargets.length === 0 ? (
          <p className="m-0 text-[var(--text-dense-meta)] text-muted-foreground">
            {matrixQuery.isLoading ? 'Loading…' : 'No trade_write targets in this matrix.'}
          </p>
        ) : (
          <>
            <p className="m-0 mb-2 text-[var(--text-dense-caption)] text-muted-foreground">
              {writeTargets.length === 1
                ? '1 write-path target blocked by policy (D10)'
                : `${writeTargets.length} write-path targets blocked by policy (D10)`}
            </p>
            <DenseDataTable>
              <DenseTableHeader>
                <DenseTableHeadRow>
                  <DenseTableHead>Target</DenseTableHead>
                  <DenseTableHead>Reach</DenseTableHead>
                  <DenseTableHead>Auth</DenseTableHead>
                  <DenseTableHead>Detail</DenseTableHead>
                </DenseTableHeadRow>
              </DenseTableHeader>
              <DenseTableBody>
                {writeTargets.map(target => (
                  <DenseTableRow
                    key={target.id}
                    className="cursor-pointer hover:bg-[var(--secondary)]"
                    onClick={() => setSelected(target)}
                  >
                    <DenseTableCell className="font-mono-tabular font-medium">{target.id}</DenseTableCell>
                    <DenseTableCell>
                      <StatusLamp value={target.reachability} kind="reach" />{' '}
                      <span className="font-mono-tabular">{target.reachability}</span>
                    </DenseTableCell>
                    <DenseTableCell>
                      <AuthCell target={target} />
                    </DenseTableCell>
                    <DenseTableCell className="max-w-xs truncate text-[var(--muted-foreground)]">
                      {target.detail || '—'}
                    </DenseTableCell>
                  </DenseTableRow>
                ))}
              </DenseTableBody>
            </DenseDataTable>
          </>
        )}
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
                  <dd className="m-0">
                    <AuthCell target={selected} />
                  </dd>
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
