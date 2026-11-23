import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Button } from '@bifrost/ui'
import { wakeComputeNode } from '@/api/clusterActuation'
import type { ClusterNode } from '@/api/clusterTypes'
import { ConfirmDialog } from '@/components/ConfirmDialog'
import { RequestPowerOffNode } from '@/components/shell/clusterActionRequests'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { Dot } from './OwnerParts'
import { errorText } from './useOwnerYours'

const GPU_NODE = 'gpu-server'

export function OwnerGpuPower({ nodes }: { nodes: ClusterNode[] }) {
  const { canOperate, canAdmin } = usePlatformAuth()
  const queryClient = useQueryClient()
  const [confirmWake, setConfirmWake] = useState(false)
  const node = nodes.find(candidate => candidate.name === GPU_NODE && candidate.compute_managed)
  const online = node?.status === 'Ready'
  const canWake = canOperate && node != null && !online
  const status = online ? 'Online' : node?.elastic_mode === 'standby' ? 'Standby' : node ? 'Not ready' : 'Status unavailable'
  const wake = useMutation({
    mutationFn: async () => {
      const result = await wakeComputeNode(GPU_NODE)
      if (!result.ok) throw new Error(result.message)
      return result
    },
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['cluster', 'nodes'] }),
  })

  return <section aria-label="4090 power" className="flex flex-wrap items-center justify-between gap-3 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-4 py-3">
    <div>
      <h2 className="m-0 text-base font-semibold">4090 power</h2>
      <p className="m-0 flex items-center gap-2 text-sm text-muted-foreground" role="status">
        <Dot tone={online ? 'ok' : 'muted'} />
        {status}
      </p>
    </div>
    <div className="flex flex-wrap items-start gap-2 [&_button]:min-h-11">
      <Button type="button" variant="outline" size="sm" disabled={!canWake || wake.isPending} onClick={() => setConfirmWake(true)}>
        Wake (WOL)
      </Button>
      <RequestPowerOffNode name={GPU_NODE} disabled={!canAdmin || !online || wake.isPending} />
    </div>
    {wake.isSuccess && !online && <p className="m-0 basis-full text-sm text-muted-foreground" role="status">{wake.data.message}</p>}
    {wake.isError && <p className="m-0 basis-full text-sm text-destructive" role="alert">{errorText(wake.error)}</p>}
    {confirmWake && <ConfirmDialog
      open
      title="Wake the 4090"
      message="Send Wake-on-LAN to gpu-server. The node may take a few minutes to come online."
      confirmLabel="Wake 4090"
      onConfirm={() => {
        setConfirmWake(false)
        if (canWake && !wake.isPending) wake.mutate()
      }}
      onCancel={() => setConfirmWake(false)}
    />}
  </section>
}
