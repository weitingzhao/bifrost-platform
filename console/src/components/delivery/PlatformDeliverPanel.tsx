import {
  Button,
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  Input,
} from '@bifrost/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { fetchSupplyChain, startPipelineRun } from '@/api/platform'
import { OpsSection } from '@/components/layout/OpsSection'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { DELIVERY_FOCUS_RUN_QUERY_KEY } from '@/lib/delivery/deliveryFocusRun'
import {
  DELIVER_PLATFORM_PIPELINE,
  PLATFORM_DOCKERFILE_CONFIGMAPS,
  PLATFORM_GITOPS_PHASE1_ITEMS,
  PLATFORM_STG_URLS,
} from '@/lib/delivery/deliverPlatformPhases'

interface PlatformDeliverPanelProps {
  /** Unused — panel loads supply chain internally. */
}

export function PlatformDeliverPanel(_props: PlatformDeliverPanelProps) {
  const { canOperate } = usePlatformAuth()
  const qc = useQueryClient()
  const [revision, setRevision] = useState('main')
  const [actionError, setActionError] = useState<string | null>(null)

  const supplyQuery = useQuery({
    queryKey: ['delivery', 'supply-chain'],
    queryFn: fetchSupplyChain,
    refetchInterval: 15_000,
  })
  const supplyChain = supplyQuery.data

  const deliverMutation = useMutation({
    mutationFn: (rev: string) => startPipelineRun(DELIVER_PLATFORM_PIPELINE, rev),
    onMutate: () => setActionError(null),
    onSuccess: data => {
      if (data.run?.name) {
        qc.setQueryData(DELIVERY_FOCUS_RUN_QUERY_KEY, data.run.name)
        void qc.invalidateQueries({ queryKey: ['delivery', 'steps', data.run.name] })
      }
      void qc.invalidateQueries({ queryKey: ['delivery', 'runs', DELIVER_PLATFORM_PIPELINE] })
      void qc.invalidateQueries({ queryKey: ['delivery', 'supply-chain'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
    },
    onError: (err: Error) => setActionError(err.message),
  })

  const cmPresent = (name: string) =>
    supplyChain?.dockerfile_configmaps?.some(cm => cm.name === name && cm.present) ?? false
  const cmCount = PLATFORM_DOCKERFILE_CONFIGMAPS.filter(exp => cmPresent(exp.name)).length
  const cmAllOk = cmCount === PLATFORM_DOCKERFILE_CONFIGMAPS.length

  return (
    <OpsSection
      title="Platform GitOps — Phase 1"
      description="Ops Platform containerization on K3s (namespace bifrost-platform-stg). Owner sign-off after verify."
    >
      <div className="flex flex-col gap-3">
        <div className="rounded-md border border-border bg-secondary/40 px-3 py-2 text-dense-meta text-muted-foreground">
          <p className="font-medium text-foreground mb-1">Phase 1 deliverables (code complete — cluster apply pending push)</p>
          <ul className="list-disc pl-4 space-y-0.5">
            {PLATFORM_GITOPS_PHASE1_ITEMS.map(item => (
              <li key={item}>{item}</li>
            ))}
          </ul>
          <p className="mt-2">
            After <code className="text-dense-caption">make k3s-deliver-platform</code>: Console{' '}
            <a href={PLATFORM_STG_URLS.console} className="text-primary underline" target="_blank" rel="noreferrer">
              {PLATFORM_STG_URLS.console}
            </a>
            · API{' '}
            <a href={PLATFORM_STG_URLS.apiHealth} className="text-primary underline" target="_blank" rel="noreferrer">
              /health
            </a>
          </p>
          <p className="mt-1 text-warning">
            Owner sign-off: Agent Briefing → Automate → platform-gitops lane — confirm Phase 1 then proceed to Phase 2.
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <DenseTag variant={cmAllOk ? 'success' : 'warning'}>
            Platform Dockerfile CMs: {cmCount}/{PLATFORM_DOCKERFILE_CONFIGMAPS.length}
          </DenseTag>
          <span className="text-dense-meta text-muted-foreground">
            Gitea mirror: {supplyChain?.tracked_repos?.includes('bifrost-platform') ? 'bifrost-platform ✓' : 'sync mirrors'}
          </span>
        </div>

        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>ConfigMap</DenseTableHead>
              <DenseTableHead>Status</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {PLATFORM_DOCKERFILE_CONFIGMAPS.map(exp => (
              <DenseTableRow key={exp.name}>
                <DenseTableCell>{exp.short}</DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={cmPresent(exp.name) ? 'success' : 'warning'}>
                    {cmPresent(exp.name) ? 'present' : 'missing'}
                  </DenseTag>
                </DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>

        {canOperate && (
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-dense-label text-muted-foreground shrink-0">Revision:</span>
            <Input
              className="h-8 w-28 text-dense-body"
              value={revision}
              onChange={e => setRevision(e.target.value)}
            />
            <Button
              size="sm"
              disabled={deliverMutation.isPending || !cmAllOk}
              onClick={() => deliverMutation.mutate(revision)}
            >
              {deliverMutation.isPending ? 'Starting…' : 'Run deliver-platform'}
            </Button>
            {!cmAllOk && (
              <span className="text-dense-meta text-muted-foreground">
                Refresh Dockerfile CMs from Supply chain above first.
              </span>
            )}
          </div>
        )}

        {actionError && <p className="text-dense-meta text-destructive">{actionError}</p>}
      </div>
    </OpsSection>
  )
}
