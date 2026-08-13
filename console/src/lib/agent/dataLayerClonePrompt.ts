import { DATA_LAYER_CLONE_SCOPE } from '@/lib/agent/agentScopes'

export const DATA_LAYER_CLONE_PLAYBOOK = 'Playbook: data-layer-clone'

export const DATA_LAYER_CLONE_CONFIRM_TOKEN = 'CLONE-FROM-PROD'

export const DATA_LAYER_CLONE_DEV_APIS = [
  'api-monitor',
  'api-market',
  'api-trading',
  'api-strategy',
  'api-portfolio',
  'api-ops',
  'api-docs',
  'api-research',
] as const

/** Operator context passed into startRemediation — runner prompt is the workflow SSOT. */
export function buildDataLayerCloneOperatorPrompt(opts?: {
  lastCloneAt?: string | null
  lagDays?: number | null
  verdict?: string | null
}): string {
  const last = opts?.lastCloneAt?.trim() || 'unknown'
  const lag =
    opts?.lagDays != null && Number.isFinite(opts.lagDays) ? `${opts.lagDays}d` : 'unknown'
  const verdict = opts?.verdict?.trim() || 'unknown'
  return [
    DATA_LAYER_CLONE_PLAYBOOK,
    '',
    `Scope: ${DATA_LAYER_CLONE_SCOPE}`,
    'Owner confirmed Refresh DEV ledger from Ops TCC ConfirmDialog.',
    `Observed: last_clone_at=${last} lag_vs_prod=${lag} verdict=${verdict}`,
    '',
    'Clone bifrost_prod → bifrost_dev only (Full). Do not touch bifrost_stg or bifrost_prod.',
    'Do not dump redis-live-prod. D10 remains BLOCKED.',
  ].join('\n')
}
