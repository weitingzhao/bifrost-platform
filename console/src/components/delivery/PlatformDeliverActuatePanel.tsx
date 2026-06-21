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
import { deliveryFocusRunQueryKey } from '@/lib/delivery/deliveryFocusRun'
import {
  PLATFORM_GITOPS_PHASE1_ITEMS,
  PLATFORM_STG_URLS,
} from '@/lib/delivery/deliverPlatformPhases'
import type { DeliveryTargetConfig } from '@/lib/delivery/deliveryTargets'

interface PlatformDeliverActuatePanelProps {
  target: DeliveryTargetConfig
}

export function PlatformDeliverActuatePanel({ target }: PlatformDeliverActuatePanelProps) {
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
    mutationFn: (rev: string) => startPipelineRun(target.pipeline, rev),
    onMutate: () => setActionError(null),
    onSuccess: data => {
      if (data.run?.name) {
        qc.setQueryData(deliveryFocusRunQueryKey(target.pipeline), data.run.name)
        void qc.invalidateQueries({ queryKey: ['delivery', 'steps', data.run.name] })
      }
      void qc.invalidateQueries({ queryKey: ['delivery', 'runs', target.pipeline] })
      void qc.invalidateQueries({ queryKey: ['delivery', 'supply-chain'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
    },
    onError: (err: Error) => setActionError(err.message),
  })

  const cmPresent = (name: string) =>
    supplyChain?.dockerfile_configmaps?.some(cm => cm.name === name && cm.present) ?? false
  const cmCount = target.dockerfileConfigMaps.filter(exp => cmPresent(exp.name)).length
  const cmAllOk = cmCount === target.dockerfileConfigMaps.length
  const mirrorsOk = target.mirrorRepos.every(repo => supplyChain?.tracked_repos?.includes(repo))

  return (
    <OpsSection
      title="Ops Platform STG — actuate"
      description="Run bifrost-deliver-platform after Dockerfile ConfigMaps are present (use Trade supply chain → Refresh Dockerfile CMs for all 7 CMs)."
    >
      <div className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <DenseTag variant={cmAllOk ? 'success' : 'warning'}>
            Dockerfile CMs: {cmCount}/{target.dockerfileConfigMaps.length}
          </DenseTag>
          <DenseTag variant={mirrorsOk ? 'success' : 'warning'}>
            Gitea mirrors: {mirrorsOk ? 'ready' : 'sync from Trade supply chain'}
          </DenseTag>
          <DenseTag variant="category">ns: {target.namespace}</DenseTag>
        </div>

        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>ConfigMap</DenseTableHead>
              <DenseTableHead>Status</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {target.dockerfileConfigMaps.map(exp => (
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

        <p className="text-dense-meta text-muted-foreground m-0">
          Smoke:{' '}
          <a href={PLATFORM_STG_URLS.console} className="text-primary underline" target="_blank" rel="noreferrer">
            Console {PLATFORM_STG_URLS.console}
          </a>
          ·{' '}
          <a href={PLATFORM_STG_URLS.apiHealth} className="text-primary underline" target="_blank" rel="noreferrer">
            API /health
          </a>
        </p>

        <details className="text-dense-meta text-muted-foreground">
          <summary className="cursor-pointer text-dense-label text-foreground">Phase 1 checklist (Owner sign-off)</summary>
          <ul className="mt-2 list-disc pl-4 space-y-0.5">
            {PLATFORM_GITOPS_PHASE1_ITEMS.map(item => (
              <li key={item}>{item}</li>
            ))}
          </ul>
        </details>

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
              {deliverMutation.isPending ? 'Starting…' : `Run ${target.pipeline}`}
            </Button>
            {!cmAllOk && (
              <span className="text-dense-meta text-muted-foreground">
                Operate → Trade STG → Refresh Dockerfile CMs (includes platform CMs).
              </span>
            )}
          </div>
        )}

        {actionError && <p className="text-dense-meta text-destructive m-0">{actionError}</p>}
      </div>
    </OpsSection>
  )
}
