import type { StgSmokeResponse, SupplyChainResponse } from '@/api/deliveryTypes'
import { worst, type Signal, type ModuleState } from '@/domains/foundation/observation-display/signals'
import { formatPipelineRunStatus, isPipelineRunFailed } from '@/lib/delivery/pipelineRunAskPack'

function stgSmokeCounts(stg?: StgSmokeResponse): { ok: number; total: number; allOk: boolean } {
  const total = stg?.targets.length ?? 0
  const ok = stg?.targets.filter(t => t.reachability === 'ok').length ?? 0
  return { ok, total, allOk: total > 0 && ok === total }
}

function smokeSignalFromCounts(smoke: { ok: number; total: number; allOk: boolean }): Signal {
  if (smoke.total === 0) return 'unknown'
  if (smoke.allOk) return 'ok'
  if (smoke.ok === 0) return 'fail'
  return 'degraded'
}

export function releaseSignal(supply?: SupplyChainResponse, stg?: StgSmokeResponse): ModuleState {
  if (!supply) return { signal: 'unknown', value: '…', detail: 'Release: probing' }
  if (supply.reachability === 'fail') {
    return { signal: 'fail', value: 'down', detail: 'Delivery pipeline unreachable' }
  }
  const last = supply.last_deliver_run
  const smoke = stgSmokeCounts(stg)
  const smokeSignal = smokeSignalFromCounts(smoke)
  const smokeDetail = smoke.total > 0 ? `STG smoke ${smoke.ok}/${smoke.total}` : ''
  if (!last) {
    return {
      signal: worst('ok', smokeSignal),
      value: 'idle',
      detail: smokeDetail !== '' ? `No recent deliver run · ${smokeDetail}` : 'No recent deliver run',
    }
  }
  const pipelineLabel = last.pipeline ?? 'bifrost-deliver-stg'
  const statusLabel = formatPipelineRunStatus(last)
  const running = statusLabel === 'Running' || (last.reason ?? '').toLowerCase() === 'running'
  if (running) {
    return {
      signal: worst('degraded', smokeSignal),
      value: 'shipping',
      detail: `Deliver running: ${pipelineLabel} ${last.name}${smokeDetail !== '' ? ` · ${smokeDetail}` : ''}`,
    }
  }
  if (isPipelineRunFailed(last)) {
    if (smoke.allOk) {
      return {
        signal: 'degraded',
        value: 'stale-fail',
        detail: `Deliver-stg failed (${last.name}: ${statusLabel}) · ${smokeDetail} — runtime OK, rerun pipeline (not cluster/nodes)`,
      }
    }
    return {
      signal: worst('fail', smokeSignal),
      value: 'failed',
      detail: `Deliver-stg failed (${last.name}: ${statusLabel})${smokeDetail !== '' ? ` · ${smokeDetail}` : ''}`,
    }
  }
  return {
    signal: worst('ok', smokeSignal),
    value: 'shipped',
    detail: `Last deliver: ${pipelineLabel} ${statusLabel}${smokeDetail !== '' ? ` · ${smokeDetail}` : ''}`,
  }
}
