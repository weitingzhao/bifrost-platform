import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Button, DenseDataTable, DenseTableBody, DenseTableCell, DenseTableHead, DenseTableHeadRow, DenseTableHeader, DenseTableRow, DenseTag, StatusLamp } from '@bifrost/ui'
import { Wrench } from 'lucide-react'
import {
  fetchCluster,
  fetchClusterPostgresStatus,
  fetchClusterServiceReadiness,
  fetchMatrix,
  fetchRetrospectiveReport,
  fetchStgSmoke,
  fetchSupplyChain,
  isAllMatrices,
} from '@/api/platform'
import type { ClusterPostgresStatusResponse, ClusterServiceReadinessResponse, ClusterSummary } from '@/api/types'
import { OpsSection } from '@/components/layout/OpsSection'
import { buildDeliverStgRecoverPrompt } from '@/lib/agent/deliverStgRecoverPrompt'
import { buildPlaybookAgentPrompt, scopeForPlaybookId } from '@/lib/agent/playbookAgentPrompts'
import {
  buildClusterFailureTriage,
  type FailureTriageRow,
  type RemediationTrack,
} from '@/lib/cluster/clusterFailureTriage'
import { buildMissionSnapshot } from '@/lib/control-room/missionSignals'
import { useMissionSnapshot } from '@/hooks/useMissionSnapshot'

const REFETCH_MS = 25_000

function trackVariant(track: RemediationTrack): 'category' | 'warning' | 'danger' | 'success' {
  switch (track) {
    case 'playbook':
      return 'category'
    case 'product':
      return 'warning'
    case 'infra':
      return 'danger'
    default:
      return 'category'
  }
}

function TriageRowActions({
  row,
  supply,
  stgSmoke,
  onOpenAgentDesk,
  onOpenDefects,
  onPlaybookFix,
  playbookFixPending,
  canOperate,
}: {
  row: FailureTriageRow
  supply?: import('@/api/types').SupplyChainResponse
  stgSmoke?: import('@/api/types').StgSmokeResponse
  onOpenAgentDesk?: (opts: { prefill: string }) => void
  onOpenDefects?: () => void
  onPlaybookFix?: (opts: { scope: string; prompt: string }) => void
  playbookFixPending?: boolean
  canOperate?: boolean
}) {
  const scope = scopeForPlaybookId(row.playbookId)
  if (scope != null && onPlaybookFix != null && canOperate) {
    const prompt =
      row.playbookId === 'deliver-stg-recover'
        ? buildDeliverStgRecoverPrompt({ supply, stgSmoke })
        : buildPlaybookAgentPrompt(row)
    return (
      <Button
        variant="ghost"
        size="xs"
        className="shrink-0"
        disabled={playbookFixPending}
        onClick={() => onPlaybookFix({ scope, prompt })}
        title={`Start ${scope} agent task`}
      >
        <Wrench size={12} className="mr-1" aria-hidden />
        Fix
      </Button>
    )
  }
  if (row.playbookId === 'deliver-stg-recover' && onOpenAgentDesk != null) {
    return (
      <Button
        variant="ghost"
        size="xs"
        className="shrink-0"
        onClick={() =>
          onOpenAgentDesk({
            prefill: buildDeliverStgRecoverPrompt({ supply, stgSmoke }),
          })
        }
        title="Open Agent Desk with deliver-stg-recover playbook"
      >
        <Wrench size={12} className="mr-1" aria-hidden />
        Fix
      </Button>
    )
  }
  if (row.retrospectiveOccurrences != null && row.retrospectiveOccurrences >= 2 && onOpenDefects != null) {
    return (
      <Button variant="ghost" size="xs" onClick={onOpenDefects}>
        Defects
      </Button>
    )
  }
  if (row.playbookId != null && onOpenAgentDesk != null) {
    return (
      <Button
        variant="ghost"
        size="xs"
        onClick={() =>
          onOpenAgentDesk({
            prefill: [
              `Playbook: ${row.playbookId}`,
              '',
              `Issue: ${row.title}`,
              `Track: ${row.track} — ${row.trackReason}`,
              '',
              'Suggested action:',
              row.suggestedAction,
              '',
              'Evidence:',
              ...row.evidence.map(e => `- ${e}`),
            ].join('\n'),
          })
        }
      >
        Agent Fix
      </Button>
    )
  }
  return null
}

export type ClusterFailureTriageStripProps = {
  summary?: ClusterSummary
  serviceReadiness?: ClusterServiceReadinessResponse
  postgresStatus?: ClusterPostgresStatusResponse
  topN?: number
  onOpenAgentDesk?: (opts: { prefill: string }) => void
  onOpenDefects?: () => void
  onPlaybookFix?: (opts: { scope: string; prompt: string }) => void
  playbookFixPending?: boolean
  canOperate?: boolean
}

export function ClusterFailureTriageStrip({
  summary: summaryProp,
  serviceReadiness: readinessProp,
  postgresStatus: postgresProp,
  topN = 8,
  onOpenAgentDesk,
  onOpenDefects,
  onPlaybookFix,
  playbookFixPending,
  canOperate,
}: ClusterFailureTriageStripProps) {
  const { snapshot, matrices, isLoading: missionLoading } = useMissionSnapshot()

  const summaryQ = useQuery({
    queryKey: ['cluster-triage', 'summary'],
    queryFn: fetchCluster,
    refetchInterval: REFETCH_MS,
    enabled: summaryProp == null,
  })
  const readinessQ = useQuery({
    queryKey: ['cluster-triage', 'service-readiness'],
    queryFn: fetchClusterServiceReadiness,
    refetchInterval: REFETCH_MS,
    enabled: readinessProp == null,
  })
  const postgresQ = useQuery({
    queryKey: ['cluster-triage', 'postgres'],
    queryFn: fetchClusterPostgresStatus,
    refetchInterval: REFETCH_MS,
    enabled: postgresProp == null,
  })
  const supplyQ = useQuery({
    queryKey: ['cluster-triage', 'supply'],
    queryFn: fetchSupplyChain,
    refetchInterval: REFETCH_MS,
  })
  const smokeQ = useQuery({
    queryKey: ['cluster-triage', 'stg-smoke'],
    queryFn: fetchStgSmoke,
    refetchInterval: REFETCH_MS,
  })
  const matricesQ = useQuery({
    queryKey: ['cluster-triage', 'matrices'],
    queryFn: async () => {
      const data = await fetchMatrix()
      return isAllMatrices(data) ? data.matrices : [data]
    },
    refetchInterval: REFETCH_MS,
    enabled: matrices.length === 0,
  })
  const retroQ = useQuery({
    queryKey: ['cluster-triage', 'retrospective'],
    queryFn: () => fetchRetrospectiveReport(false),
    refetchInterval: 60_000,
  })

  const summary = summaryProp ?? summaryQ.data
  const serviceReadiness = readinessProp ?? readinessQ.data
  const postgresStatus = postgresProp ?? postgresQ.data

  const missionSnapshot = useMemo(() => {
    if (summary == null) return snapshot
    return buildMissionSnapshot({
      cluster: summary,
      supply: supplyQ.data,
      stg: smokeQ.data,
      matrices: matricesQ.data ?? matrices,
    })
  }, [summary, snapshot, supplyQ.data, smokeQ.data, matricesQ.data, matrices])

  const rows = useMemo(() => {
    if (summary == null) return []
    return buildClusterFailureTriage({
      summary,
      serviceReadiness,
      postgresStatus,
      missionSnapshot,
      supplyChain: supplyQ.data,
      stgSmoke: smokeQ.data,
      matrices: matricesQ.data ?? matrices,
      retrospectivePatterns: retroQ.data?.patterns ?? [],
      topN,
    })
  }, [
    summary,
    serviceReadiness,
    postgresStatus,
    missionSnapshot,
    supplyQ.data,
    smokeQ.data,
    matricesQ.data,
    matrices,
    retroQ.data?.patterns,
    topN,
  ])

  const isLoading =
    missionLoading ||
    (summaryProp == null && summaryQ.isLoading) ||
    supplyQ.isLoading

  if (isLoading && rows.length === 0) {
    return (
      <OpsSection title="Failure triage (Top N)" description="Ranking cluster, release, and retrospective patterns…">
        <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-muted-foreground">Loading triage…</p>
      </OpsSection>
    )
  }

  if (rows.length === 0) {
    return (
      <OpsSection
        title="Failure triage (Top N)"
        leading={<StatusLamp value="ok" kind="reach" />}
        description="No ranked failures — nodes ready, no blocking release or domain gaps in triage scope."
      >
        <p className="m-0 text-[var(--text-dense-meta)] text-muted-foreground">
          Run <code className="font-mono text-xs">make cluster-triage</code> for a full markdown report.
        </p>
      </OpsSection>
    )
  }

  const hasCritical = rows.some(r => r.severity === 'fail')

  return (
    <OpsSection
      title="Failure triage (Top N)"
      leading={<StatusLamp value={hasCritical ? 'fail' : 'degraded'} kind="reach" />}
      description="ROI-ranked issues — playbook vs product vs infra. Release failures are distinct from K8s node health."
      bodyPadding="none"
      overflow="visible"
      bodyClassName="ops-section-body--table"
      actions={
        onOpenDefects != null ? (
          <Button variant="outline" size="sm" onClick={onOpenDefects}>
            Defects →
          </Button>
        ) : undefined
      }
    >
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead className="w-[5%]">#</DenseTableHead>
            <DenseTableHead className="w-[8%]">Sev</DenseTableHead>
            <DenseTableHead className="w-[10%]">Track</DenseTableHead>
            <DenseTableHead className="w-[28%]">Issue</DenseTableHead>
            <DenseTableHead className="w-[8%]">Retro</DenseTableHead>
            <DenseTableHead>Suggested action</DenseTableHead>
            <DenseTableHead className="w-[10%]" />
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {rows.map(row => (
            <DenseTableRow key={row.id}>
              <DenseTableCell className="font-mono tabular-nums">{row.rank}</DenseTableCell>
              <DenseTableCell>
                <DenseTag variant={row.severity === 'fail' ? 'danger' : 'warning'}>{row.severity}</DenseTag>
              </DenseTableCell>
              <DenseTableCell>
                <DenseTag variant={trackVariant(row.track)}>{row.track}</DenseTag>
              </DenseTableCell>
              <DenseTableCell className="font-medium">{row.title}</DenseTableCell>
              <DenseTableCell className="font-mono tabular-nums text-muted-foreground">
                {row.retrospectiveOccurrences != null ? `${row.retrospectiveOccurrences}×` : '—'}
              </DenseTableCell>
              <DenseTableCell className="cluster-issues-cell-clip text-muted-foreground" title={row.suggestedAction}>
                {row.suggestedAction}
              </DenseTableCell>
              <DenseTableCell>
                <TriageRowActions
                  row={row}
                  supply={supplyQ.data}
                  stgSmoke={smokeQ.data}
                  onOpenAgentDesk={onOpenAgentDesk}
                  onOpenDefects={onOpenDefects}
                  onPlaybookFix={onPlaybookFix}
                  playbookFixPending={playbookFixPending}
                  canOperate={canOperate}
                />
              </DenseTableCell>
            </DenseTableRow>
          ))}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}
