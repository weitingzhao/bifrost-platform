import type {
  DeliveryPipelineRunView,
  DeliveryPipelineRunsResponse,
  PipelineRunStepsResponse,
  ReleaseStateResponse,
  SelfHealthResponse,
  SupplyChainResponse,
} from '@/api/types'

export interface DeployDebugBundleInput {
  target: string
  pipeline: string
  namespace: string
  revision: string
  /** The mutation error message (e.g. K8s label validation). */
  actionError?: string | null
  run?: DeliveryPipelineRunView
  runs?: DeliveryPipelineRunsResponse
  steps?: PipelineRunStepsResponse
  supplyChain?: SupplyChainResponse
  releaseState?: ReleaseStateResponse
  selfHealth?: SelfHealthResponse
}

function row(cells: string[]): string {
  return `| ${cells.join(' | ')} |`
}

/**
 * Builds a self-contained markdown report for a failing deploy PipelineRun
 * or a failed deploy action (mutation error). Designed to be pasted into
 * an AI assistant for root-cause analysis.
 */
export function buildDeployDebugBundle(input: DeployDebugBundleInput): string {
  const {
    target, pipeline, namespace, revision,
    actionError, run, runs, steps,
    supplyChain, releaseState, selfHealth,
  } = input

  const effectiveRevision = (run?.revision?.trim() || revision.trim()) || 'unknown'

  const lines: string[] = [
    `# Platform deploy debug bundle — ${target}`,
    '',
    `Generated: ${new Date().toISOString()}`,
    'Mode: Ops',
    'Source: Ops Console → Operate → Platform Release',
    `Target: ${target} · Pipeline: ${pipeline} · Namespace: ${namespace}`,
    `Revision: ${effectiveRevision}`,
  ]

  if (actionError) {
    lines.push(
      '',
      '## Deploy action error (API / mutation)',
      '```',
      actionError,
      '```',
    )
  }

  lines.push('', '## Failed PipelineRun')
  if (run == null) {
    lines.push('- No PipelineRun data available (the run may not have been created).')
  } else {
    lines.push(
      `- name: ${run.name}`,
      `- status: ${run.status}${run.reason ? ` (${run.reason})` : ''}`,
      `- revision: ${run.revision ?? '—'}`,
      `- started: ${run.start_time || '—'}`,
      `- completed: ${run.completion_time || '—'}`,
    )
  }

  const phases = steps?.phases ?? []
  if (phases.length > 0) {
    const failed = phases.filter(p => p.status === 'failed')
    lines.push(
      '',
      '## Pipeline phases',
      '',
      row(['Phase', 'Status', 'Detail']),
      row(['---', '---', '---']),
      ...phases.map(p => row([
        p.label,
        p.status === 'failed' ? '**FAILED**' : p.status,
        (p.detail ?? '').replace(/\|/g, '\\|'),
      ])),
    )
    if (failed.length > 0) {
      lines.push('', `Failed phases: ${failed.map(p => p.label).join(', ')}`)
    }
  }

  const tasks = steps?.tasks ?? []
  const failedTasks = tasks.filter(t =>
    t.status.toLowerCase() === 'failed',
  )
  if (failedTasks.length > 0) {
    lines.push(
      '',
      '## Failed tasks (root cause candidates)',
      '',
      ...failedTasks.map(t => [
        `### ${t.pipeline_task}`,
        `- TaskRun: ${t.name}`,
        `- Status: ${t.status}${t.reason ? ` (${t.reason})` : ''}`,
      ].join('\n')),
    )
  }

  if (tasks.length > 0) {
    lines.push(
      '',
      '## All tasks',
      '',
      row(['Task', 'Name', 'Status', 'Reason']),
      row(['---', '---', '---', '---']),
      ...tasks.map(t => row([
        t.pipeline_task,
        t.name,
        t.status,
        (t.reason ?? '').replace(/\|/g, '\\|'),
      ])),
    )
  }

  if (supplyChain != null) {
    const cms = supplyChain.dockerfile_configmaps ?? []
    const missing = cms.filter(cm => !cm.present)
    lines.push(
      '',
      '## Supply chain',
      `- Mirror credentials: ${supplyChain.mirror_credentials_configured ? 'configured' : 'NOT configured'}`,
      `- Default revision: ${supplyChain.default_revision}`,
      `- Dockerfile ConfigMaps: ${cms.filter(cm => cm.present).length}/${cms.length} present`,
    )
    if (missing.length > 0) {
      lines.push(`- Missing: ${missing.map(cm => cm.name).join(', ')}`)
    }
  }

  if (releaseState != null) {
    lines.push(
      '',
      '## Release state',
      `- STG Deploy: ${releaseState.stg_deploy.status} (${releaseState.stg_deploy.revision ?? '—'})`,
      `- STG Gate: ${releaseState.stg_gate.status}`,
      `- PROD Deploy: ${releaseState.prod_deploy.status} (${releaseState.prod_deploy.revision ?? '—'})`,
      `- PROD Gate: ${releaseState.prod_gate.status}`,
      `- Consistent: ${releaseState.consistent ? 'yes' : 'no'}`,
    )
  }

  lines.push('', `## Platform self-health (overall: ${selfHealth?.overall ?? 'unknown'})`)
  const probes = selfHealth?.probes ?? []
  if (probes.length > 0) {
    lines.push(
      '',
      row(['Component', 'Env', 'Status', 'Detail']),
      row(['---', '---', '---', '---']),
      ...probes.map(p => row([p.category, p.env, p.status, p.detail.replace(/\|/g, '\\|')])),
    )
  }

  if (runs != null && runs.runs.length > 1) {
    lines.push(
      '',
      '## Recent runs',
      ...runs.runs.slice(0, 5).map(r =>
        `- ${r.name}: ${r.status}${r.reason ? ` (${r.reason})` : ''} rev=${r.revision ?? '—'}`,
      ),
    )
  }

  lines.push(
    '',
    '## Ask',
    `The ${target} deploy failed with revision "${effectiveRevision}". ` +
      (actionError
        ? 'The deploy action itself returned an error before the PipelineRun could start. '
        : 'The PipelineRun was created but failed during execution. ') +
      'Using the error details, failed tasks, pipeline phases, supply chain state, and ' +
      'self-health probes above, identify the root cause and propose concrete remediation ' +
      'steps (kubectl commands, ConfigMap fixes, label sanitization, or Console actions). ' +
      'Distinguish the primary failure from downstream effects.',
  )

  return lines.join('\n')
}
