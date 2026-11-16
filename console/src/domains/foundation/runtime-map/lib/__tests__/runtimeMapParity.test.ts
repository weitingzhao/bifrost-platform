import { describe, expect, it } from 'vitest'
import type { MatrixResponse, TopologyNode, TopologyResponse } from '@/api/matrixTypes'
import { chipsForNode, chipIdsMatchingTarget, componentIdForScopeTag } from '../infraVisualRegistry'
import { buildScopeLayers, type ScopeTag } from '../runtimeMapRegistry'
import { buildGapOverview } from '../gapAnalysis'
import { buildRuntimeLlmPack } from '../buildRuntimeLlmPack'

// Frozen from e0b80785 before extraction: exercise overlapping role matchers,
// tenant prefixes, unknown/planned, ghost merging and exact agent-pack text.
const roles = ['postgres', 'cnpg', 'redis', 'ingress', 'prod_compose', 'k3s', 'api', 'gitea', 'argocd', 'tekton', 'monitoring', 'tws', 'ib_host', 'ollama', 'celery', 'socket', 'frontend', 'platform_console', 'kubectl', 'mcp_client', 'ci', 'planned_staging', 'dev_workstation', 'postgres_ib', 'compose_socket', 'tws_socket', 'unknown']
function fixture(reach: 'ok' | 'degraded' | 'fail' | 'unknown') {
  const services = ['ops-capabilities', 'api-monitor', 'api-ops', 'postgres', 'redis', 'nginx-spa'].map(id => ({ id, reachability: reach, detail: 'fixture' }))
  const node = { id: 'fixture', label: 'Fixture', group: 'linux', host: 'example.invalid', status: reach, detail: 'fixture', compose_roles: roles, k3s_roles: ['api', 'tws', 'platform_console', 'planned_staging'], matrix_services: services } as TopologyNode
  const topology = { environment: 'stg', label: 'Fixture', deployment_phase: 'compose', generated_at: '2026-11-16T00:00:00Z', nodes: [node], edges: [] } as TopologyResponse
  const matrix = { environment: 'stg', label: 'Fixture', targets: services } as MatrixResponse
  return { node, topology, matrix }
}
describe('runtime map extraction parity', () => {
  it('preserves ordered chip matches, ghosts and all reach states', () => {
    expect(['ok', 'degraded', 'fail', 'unknown'].map(reach => {
      const { node, topology } = fixture(reach as 'ok')
      return { reach, compose: chipsForNode(node, 'compose').map(c => [c.chipId,c.reachability,c.matrixTargetId,c.planned]), k3s: chipsForNode(node, 'k3s', { showGhostOverlay: true }).map(c => [c.chipId,c.reachability,c.matrixTargetId,c.planned,c.ghost]), highlights: chipIdsMatchingTarget(topology, 'api-ops') }
    })).toMatchSnapshot()
  })
  it('preserves scope ordering and planned/unknown projections', () => {
    const { topology, matrix } = fixture('unknown')
    expect({ scopes: buildScopeLayers(matrix), gap: buildGapOverview(topology,matrix,'compose'), icons: ['PLATFORM','TRADE-API','SOCKET','TWS','TRADE-FE'].map(t => componentIdForScopeTag(t as ScopeTag)) }).toMatchSnapshot()
  })
  it('preserves exact agent pack text including tenant target guidance', () => {
    const { topology, matrix } = fixture('fail')
    expect([null, {kind:'target',id:'api-ops'}, {kind:'node',id:'fixture'}, {kind:'scope',tag:'TWS'}].map(selection => buildRuntimeLlmPack(topology,matrix,undefined,selection as Parameters<typeof buildRuntimeLlmPack>[3],buildGapOverview(topology,matrix,'compose')))).toMatchSnapshot()
  })
})
