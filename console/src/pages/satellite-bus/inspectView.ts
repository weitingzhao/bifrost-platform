import type { BusNodeHealth } from '@/lib/satellite-bus/satelliteBusViewModel'
import type { InspectTarget } from '@/pages/satellite-bus/inspectTypes'

export type InspectView = {
  title: string
  scopeLabel: string
  health: BusNodeHealth
  stateLabel: string
  headline?: string
  detail: string
  probePath: string
  raw?: unknown
}

export function inspectView(target: InspectTarget): InspectView {
  if (target.kind === 'node') {
    const n = target.node
    return {
      title: n.label,
      scopeLabel: n.scopeLabel,
      health: n.health,
      stateLabel: n.stateLabel,
      headline: n.headline,
      detail: n.detail,
      probePath: n.probePath,
      raw: n.raw,
    }
  }
  if (target.kind === 'consumer') {
    const r = target.row
    return {
      title: r.label,
      scopeLabel: r.kind === 'data-path' ? 'DATA PATH' : 'RUNTIME',
      health: r.health,
      stateLabel: r.stateLabel,
      detail: r.detail,
      probePath: r.probePath,
      raw: r.raw,
    }
  }
  const i = target.issue
  return {
    title: i.title,
    scopeLabel: i.scope === 'shared' ? 'SHARED' : i.scope === 'cross-env' ? `CROSS-ENV · ${i.envLabel}` : i.envLabel,
    health: i.severity === 'critical' ? 'fail' : 'degraded',
    stateLabel: i.severity.toUpperCase(),
    detail: i.detail,
    probePath: i.probePath,
    raw: i.raw,
  }
}
