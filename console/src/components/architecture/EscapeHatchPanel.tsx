import {
  Button,
  ConfirmDialog,
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  StatusLamp,
} from '@bifrost/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { fetchEscapeHatch, recordEscapeHatchDrill } from '@/api/core'
import type { EscapeRouteStatus } from '@/api/matrixTypes'
import { OpsSection, OpsSubsectionTitle } from '@/components/layout/OpsSection'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import {
  ESCAPE_HATCH_QUARTERLY_INTERVAL_DAYS,
  ESCAPE_HATCH_RUNBOOK_STEPS,
} from '@/lib/architecture/escapeHatchCatalog'

const ROUTE_LAMP: Record<EscapeRouteStatus, 'ok' | 'degraded' | 'fail' | 'unknown'> = {
  ok: 'ok',
  degraded: 'degraded',
  fail: 'fail',
  unknown: 'unknown',
  documented: 'degraded',
}

const ROUTE_TAG: Record<
  EscapeRouteStatus,
  { variant: 'success' | 'warning' | 'danger' | 'category' | 'info'; label: string }
> = {
  ok: { variant: 'success', label: 'ok' },
  degraded: { variant: 'warning', label: 'degraded' },
  fail: { variant: 'danger', label: 'fail' },
  unknown: { variant: 'category', label: 'unknown' },
  documented: { variant: 'info', label: 'documented' },
}

export function EscapeHatchPanel() {
  const qc = useQueryClient()
  const { canAdmin } = usePlatformAuth()
  const [confirmOpen, setConfirmOpen] = useState(false)

  const query = useQuery({
    queryKey: ['platform', 'escape-hatch'],
    queryFn: fetchEscapeHatch,
    refetchInterval: 30_000,
  })

  const drillMutation = useMutation({
    mutationFn: () =>
      recordEscapeHatchDrill({
        notes: 'Quarterly escape hatch drill recorded from Launch Rocket panel',
        route_ids: query.data?.routes.map(r => r.id),
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['platform', 'escape-hatch'] })
      setConfirmOpen(false)
    },
  })

  const data = query.data
  const overall = data?.overall ?? 'unknown'
  const routes = data?.routes ?? []
  const quarterly = data?.quarterly

  return (
    <>
      <OpsSection
        title="L0 escape hatch — recovery visibility"
        leading={<StatusLamp value={ROUTE_LAMP[overall]} kind="reach" />}
        description="Two independent L1 recovery paths when cluster pipeline or Console is down. Quarterly drill schedule (90 days)."
        actions={
          canAdmin ? (
            <Button
              size="sm"
              variant="outline"
              disabled={drillMutation.isPending}
              onClick={() => setConfirmOpen(true)}
            >
              {drillMutation.isPending ? 'Recording…' : 'Record quarterly drill'}
            </Button>
          ) : undefined
        }
        bodyPadding="default"
        overflow="visible"
      >
        {query.isLoading && (
          <p className="m-0 text-dense-meta text-muted-foreground">Loading escape hatch probes…</p>
        )}
        {query.isError && (
          <p className="m-0 text-dense-meta text-destructive">
            {query.error instanceof Error ? query.error.message : 'Failed to load escape hatch'}
          </p>
        )}

        {quarterly != null && (
          <div className="mb-3 flex flex-wrap items-center gap-2">
            <DenseTag variant={quarterly.overdue ? 'warning' : 'success'}>
              {quarterly.overdue ? 'Drill overdue' : 'Drill schedule OK'}
            </DenseTag>
            <span className="text-dense-meta text-muted-foreground">
              Interval {quarterly.interval_days ?? ESCAPE_HATCH_QUARTERLY_INTERVAL_DAYS}d
              {quarterly.last_drill_at != null
                ? ` · Last drill ${new Date(quarterly.last_drill_at).toLocaleDateString()} by ${quarterly.last_drill_by ?? 'owner'}`
                : ' · No drill recorded yet'}
              {quarterly.next_due_at != null && !quarterly.overdue
                ? ` · Next due ${new Date(quarterly.next_due_at).toLocaleDateString()}`
                : ''}
            </span>
          </div>
        )}

        {routes.length > 0 && (
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Route</DenseTableHead>
                <DenseTableHead>Layer</DenseTableHead>
                <DenseTableHead>Status</DenseTableHead>
                <DenseTableHead>Detail</DenseTableHead>
                <DenseTableHead>Command</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {routes.map(route => {
                const tag = ROUTE_TAG[route.status]
                return (
                  <DenseTableRow key={route.id}>
                    <DenseTableCell>
                      <div className="flex flex-col gap-0.5">
                        <span className="font-medium">{route.label}</span>
                        <span className="text-dense-meta text-muted-foreground">{route.summary}</span>
                      </div>
                    </DenseTableCell>
                    <DenseTableCell>
                      <DenseTag variant="category">{route.layer}</DenseTag>
                    </DenseTableCell>
                    <DenseTableCell>
                      <DenseTag variant={tag.variant}>{tag.label}</DenseTag>
                    </DenseTableCell>
                    <DenseTableCell className="text-dense-meta text-muted-foreground">
                      {route.detail}
                    </DenseTableCell>
                    <DenseTableCell>
                      {route.command != null && route.command !== '' ? (
                        <code className="block max-w-xs truncate font-mono text-dense-caption">
                          {route.command}
                        </code>
                      ) : (
                        '—'
                      )}
                    </DenseTableCell>
                  </DenseTableRow>
                )
              })}
            </DenseTableBody>
          </DenseDataTable>
        )}

        <OpsSubsectionTitle>Runbook</OpsSubsectionTitle>
        <ol className="m-0 list-decimal pl-5 text-dense-meta text-muted-foreground">
          {ESCAPE_HATCH_RUNBOOK_STEPS.map(step => (
            <li key={step.order} className="mb-1">
              <span className="font-medium text-foreground">{step.title}</span> — {step.detail}
            </li>
          ))}
        </ol>

        {data?.agent_guidance != null && data.agent_guidance !== '' && (
          <p className="m-0 mt-3 text-dense-meta text-muted-foreground">{data.agent_guidance}</p>
        )}
        {data?.generated_at != null && (
          <p className="m-0 mt-2 text-dense-caption text-muted-foreground">
            Last probe: {new Date(data.generated_at).toLocaleString()} · runbook {data.runbook_version}
          </p>
        )}
      </OpsSection>

      <ConfirmDialog
        open={confirmOpen}
        title="Record escape hatch drill"
        message="Record that you exercised the L0/L1 escape hatch runbook (quarterly schedule). This is persisted on platform-api."
        confirmLabel="Record drill"
        confirming={drillMutation.isPending}
        onConfirm={() => drillMutation.mutate()}
        onCancel={() => setConfirmOpen(false)}
      />
    </>
  )
}
