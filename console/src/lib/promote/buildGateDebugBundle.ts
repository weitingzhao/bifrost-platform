import type {
  DeliveryPipelineRunsResponse,
  ReleaseGateResponse,
  SelfHealthResponse,
} from '@/api/types'

export interface GateDebugBundleInput {
  tier: string
  /** Short stage label, e.g. "STG" / "PROD". */
  label: string
  pipeline: string
  namespace: string
  gate?: ReleaseGateResponse
  runs?: DeliveryPipelineRunsResponse
  selfHealth?: SelfHealthResponse
}

function tableRow(cells: string[]): string {
  return `| ${cells.join(' | ')} |`
}

/**
 * Builds a self-contained, AI-debuggable markdown report for a failing
 * Platform release gate. Everything an assistant needs to triage is inlined:
 * gate verdict, every check + detail, the latest deliver PipelineRun, and the
 * platform self-health probes.
 */
export function buildGateDebugBundle(input: GateDebugBundleInput): string {
  const { tier, label, pipeline, namespace, gate, runs, selfHealth } = input

  const lines: string[] = [
    `# Platform release gate debug bundle — ${label}`,
    '',
    `Generated: ${new Date().toISOString()}`,
    'Mode: Ops',
    'Source: Ops Console → Operate → Platform Release',
    `Tier: ${tier} · Pipeline: ${pipeline} · Namespace: ${namespace}`,
  ]

  // --- Gate verdict ---------------------------------------------------------
  lines.push('', '## Gate verdict')
  if (gate == null) {
    lines.push('- No gate record available (gate has not been run yet).')
  } else {
    lines.push(
      `- result: ${gate.result || 'not recorded'}`,
      `- ready: ${gate.ready ? 'yes' : 'no'}`,
      `- reachability: ${gate.reachability}`,
      `- detail: ${gate.detail || '—'}`,
      `- last run: ${gate.at || '—'}`,
    )
    if (gate.blockers != null && gate.blockers.length > 0) {
      lines.push(`- blockers: ${gate.blockers.join(' · ')}`)
    }
  }

  // --- Failing required checks (the most important part) --------------------
  const checks = gate?.checks ?? []
  const failingRequired = checks.filter(c => c.required && c.reachability === 'fail')
  const failingOptional = checks.filter(c => !c.required && c.reachability === 'fail')

  lines.push('', '## Failing checks (root cause candidates)')
  if (failingRequired.length === 0 && failingOptional.length === 0) {
    lines.push('- No check reported `fail`. Gate may be blocked by a milestone/blocker (see verdict above).')
  } else {
    for (const c of failingRequired) {
      lines.push(`- [REQUIRED] ${c.label} (${c.id}) — ${c.detail}`)
    }
    for (const c of failingOptional) {
      lines.push(`- [optional] ${c.label} (${c.id}) — ${c.detail}`)
    }
  }

  // --- Full check table -----------------------------------------------------
  if (checks.length > 0) {
    lines.push(
      '',
      '## All checks',
      '',
      tableRow(['Check', 'ID', 'Scope', 'Status', 'Detail']),
      tableRow(['---', '---', '---', '---', '---']),
      ...checks.map(c =>
        tableRow([
          c.label,
          c.id,
          c.required ? 'required' : 'optional',
          c.reachability,
          c.detail.replace(/\|/g, '\\|'),
        ]),
      ),
    )
  }

  // --- Latest deliver pipeline run ------------------------------------------
  lines.push('', `## Latest deliver PipelineRun (${pipeline})`)
  const latest = runs?.runs?.[0]
  if (latest == null) {
    lines.push('- No PipelineRun found for this pipeline.')
  } else {
    lines.push(
      `- name: ${latest.name}`,
      `- status: ${latest.status}${latest.reason != null && latest.reason !== '' ? ` (${latest.reason})` : ''}`,
      `- started: ${latest.start_time || '—'}`,
      `- completed: ${latest.completion_time || '—'}`,
    )
    if (runs != null && runs.runs.length > 1) {
      lines.push(
        '',
        'Recent runs:',
        ...runs.runs.slice(0, 5).map(r => `- ${r.name}: ${r.status}${r.reason ? ` (${r.reason})` : ''}`),
      )
    }
  }

  // --- Platform self-health -------------------------------------------------
  lines.push('', `## Platform self-health (overall: ${selfHealth?.overall ?? 'unknown'})`)
  const probes = selfHealth?.probes ?? []
  if (probes.length === 0) {
    lines.push('- No self-health probes available.')
  } else {
    lines.push(
      '',
      tableRow(['Component', 'Env', 'Status', 'Detail', 'Latency(ms)']),
      tableRow(['---', '---', '---', '---', '---']),
      ...probes.map(p =>
        tableRow([p.category, p.env, p.status, p.detail.replace(/\|/g, '\\|'), String(p.latency_ms)]),
      ),
    )
  }

  // --- Ask ------------------------------------------------------------------
  lines.push(
    '',
    '## Ask',
    `The Platform ${label} release gate is failing. Using the gate verdict, failing checks, ` +
      'the latest deliver PipelineRun, and self-health probes above, identify the most likely ' +
      'root cause and propose concrete, ordered remediation steps (kubectl / Console actions). ' +
      'Call out which failing check is the true blocker vs. downstream symptoms.',
  )

  return lines.join('\n')
}
