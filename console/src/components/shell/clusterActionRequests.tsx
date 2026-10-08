import { RequestActionButton } from '@/components/shell/RequestActionButton'

const DRAIN_BODY = { force: true, grace_period_seconds: 60 }

function nodeActionPath(name: string, leaf: string): string {
  return `/api/v1/cluster/nodes/${encodeURIComponent(name)}/${leaf}`
}

export function RequestCordonNode({ name, disabled }: { name: string; disabled?: boolean }) {
  return (
    <RequestActionButton
      action="cordon_node"
      params={{ name }}
      reason={`Cordon ${name}`}
      rollback="uncordon"
      label="Request cordon"
      disabled={disabled}
      direct={{ method: 'POST', path: nodeActionPath(name, 'cordon') }}
    />
  )
}

export function RequestUncordonNode({ name, disabled }: { name: string; disabled?: boolean }) {
  return (
    <RequestActionButton
      action="uncordon_node"
      params={{ name }}
      reason={`Uncordon ${name}`}
      label="Request uncordon"
      disabled={disabled}
      direct={{ method: 'POST', path: nodeActionPath(name, 'uncordon') }}
    />
  )
}

export function RequestDrainNode({ name, disabled }: { name: string; disabled?: boolean }) {
  return (
    <RequestActionButton
      action="drain_node"
      params={{ name, ...DRAIN_BODY }}
      reason={`Drain ${name}`}
      label="Request drain"
      disabled={disabled}
      direct={{ method: 'POST', path: nodeActionPath(name, 'drain'), body: DRAIN_BODY }}
    />
  )
}

export function RequestPowerOffNode({ name, disabled }: { name: string; disabled?: boolean }) {
  return (
    <RequestActionButton
      action="poweroff_compute_node"
      params={{ name }}
      reason={`Power off ${name}`}
      label="Request power off"
      disabled={disabled}
      direct={{ method: 'POST', path: nodeActionPath(name, 'poweroff') }}
    />
  )
}

export function RequestScaleDeployment({
  namespace,
  name,
  replicas,
  label,
  disabled,
}: {
  namespace: string
  name: string
  replicas: number
  label: string
  disabled?: boolean
}) {
  const body = { namespace, kind: 'Deployment', name, replicas }
  return (
    <RequestActionButton
      action="scale_deployment"
      params={body}
      reason={`Scale ${namespace}/${name} to ${replicas}`}
      label={label}
      disabled={disabled}
      direct={{ method: 'POST', path: '/api/v1/cluster/workloads/scale', body }}
    />
  )
}

export function RequestRestartDeployment({
  namespace,
  name,
  label = 'Request restart',
  disabled,
}: {
  namespace: string
  name: string
  label?: string
  disabled?: boolean
}) {
  const body = { namespace, kind: 'Deployment', name }
  return (
    <RequestActionButton
      action="rollout_restart_deployment"
      params={body}
      reason={`Rollout restart ${namespace}/${name}`}
      label={label}
      disabled={disabled}
      direct={{ method: 'POST', path: '/api/v1/cluster/workloads/rollout-restart', body }}
    />
  )
}

export function RequestSyncKubeconfig({ disabled }: { disabled?: boolean }) {
  return (
    <RequestActionButton
      action="sync_kubeconfig"
      reason="Sync kubeconfig onto the platform path"
      label="Request kubeconfig sync"
      disabled={disabled}
      direct={{ method: 'POST', path: '/api/v1/cluster/sync-kubeconfig' }}
    />
  )
}

export function RequestEnsureMetricsServer({ disabled }: { disabled?: boolean }) {
  return (
    <RequestActionButton
      action="ensure_metrics_server"
      reason="Install metrics-server"
      label="Request metrics-server"
      disabled={disabled}
      direct={{ method: 'POST', path: '/api/v1/cluster/addons/metrics-server/ensure' }}
    />
  )
}

export function RequestEnsureKubePrometheus({ disabled }: { disabled?: boolean }) {
  return (
    <RequestActionButton
      action="ensure_kube_prometheus_stack"
      reason="Install kube-prometheus-stack"
      label="Request Layer B install"
      disabled={disabled}
      direct={{ method: 'POST', path: '/api/v1/cluster/addons/kube-prometheus-stack/ensure' }}
    />
  )
}

export function RequestFirewallApply({
  includeDefaultDeny,
  disabled,
}: {
  includeDefaultDeny: boolean
  disabled?: boolean
}) {
  const body = { include_default_deny: includeDefaultDeny }
  return (
    <RequestActionButton
      action="unifi_firewall_apply"
      params={body}
      reason="Apply UniFi firewall policy"
      label="Request firewall apply"
      disabled={disabled}
      direct={{ method: 'POST', path: '/api/v1/network/firewall/apply', body }}
    />
  )
}

export function RequestDataClone({
  mode,
  tables,
  disabled,
}: {
  mode: 'full' | 'selective'
  tables?: string[]
  disabled?: boolean
}) {
  const body = {
    source: 'bifrost_prod',
    targets: ['bifrost_dev', 'bifrost_stg'],
    mode,
    tables: mode === 'selective' ? tables : undefined,
    confirmation_token: 'CLONE-FROM-PROD',
    confirm: true,
  }
  return (
    <RequestActionButton
      action="trigger_data_clone"
      params={body}
      reason={`Clone bifrost_prod (${mode})`}
      label="Request sync from Prod"
      disabled={disabled}
      direct={{ method: 'POST', path: '/api/v1/cluster/data-clone', body }}
    />
  )
}

export function RequestDataCloneSchedule({
  enabled,
  disabled,
}: {
  enabled: boolean
  disabled?: boolean
}) {
  const body = {
    enabled,
    interval: enabled ? 'weekly' : 'disabled',
    source: 'bifrost_prod',
    targets: ['bifrost_dev', 'bifrost_stg'],
    mode: 'full',
  }
  return (
    <RequestActionButton
      action="update_data_clone_schedule"
      params={body}
      reason={enabled ? 'Enable weekly data clone' : 'Disable weekly data clone'}
      label={enabled ? 'Request enable weekly' : 'Request disable weekly'}
      disabled={disabled}
      direct={{ method: 'PUT', path: '/api/v1/cluster/data-clone/schedule', body }}
    />
  )
}
