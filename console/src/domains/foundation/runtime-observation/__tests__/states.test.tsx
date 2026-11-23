import { describe, it, expect } from 'vitest'
import { COVERAGE, DATABASES, GITOPS, WORKLOADS, databasePanels, gitopsPanel, readCoverage, readStates, workloadPanels } from '../states'

// 2026-11-23 UI / Signals: the bus's v2 workload version and database backup fields, and the metrics
// line's metric_coverage_by_namespace, were in the system but not on the Signals page. Shapes below are
// the PROD responses of that day, trimmed.
const workloads = { type: 'cluster.workload.changed', ref: '/api/v1/cluster/workloads', states: [
  { subject: 'research/research-api', state: { ready: 'ready', version: 'bifrost-research:0.205.0' }, at: '2026-11-22T03:52:05Z', event: 'ev-1' },
  { subject: 'research/dagster-daemon', state: { ready: 'notready', version: 'bifrost-research:0.205.0-dagster' }, at: '2026-11-22T03:52:06Z', event: 'ev-2' },
] }
const databases = { type: 'cluster.database.changed', ref: '/api/v1/cluster/postgres', states: [
  { subject: 'data/bifrost-postgres', state: { ready: 'ready', primary: 'bifrost-postgres-1', backup: 'ok', archiving: 'ok', last_backup: '2026-11-23T04:48:34Z' }, at: '2026-11-23T04:48:47Z', event: 'ev-3' },
] }
const readout = { window: { from: '2026-11-22T06:00:00Z', to: '2026-11-23T06:00:00Z' }, readings: [
  { name: 'metric_coverage_by_namespace', labels: { namespace: 'research' }, value: 0.2, status: 'ok', note: 'no metrics of their own: dagster-daemon' },
  { name: 'metric_coverage_by_namespace', labels: { namespace: 'cicd' }, value: 0.3, status: 'ok' },
] }

describe('bus states and coverage on Signals', () => {
  it('shows the sources as they report, by namespace, without judging', async () => {
    const s = await readStates(async p => p === WORKLOADS ? workloads : p === DATABASES ? databases : undefined)
    const c = await readCoverage(async p => p === COVERAGE ? readout : undefined)
    const evidence = { ...s.evidence, ...c.evidence }
    const panels = workloadPanels(s.workloads, c.readout, evidence)
    expect(panels.map(p => p.label)).toEqual(['cicd', 'research'])
    const research = JSON.stringify(panels[1])
    expect(research).toContain('"label":"Ready","value":1')
    expect(research).toContain('"text":"1","tone":"warn"')
    expect(research).toContain('20%')
    expect(research).toContain('no metrics of their own: dagster-daemon')
    expect(research).toContain('dagster-daemon · notready · bifrost-research:0.205.0-dagster')
    // A namespace only the readout names is shown, and says the bus listed nothing for it.
    expect(JSON.stringify(panels[0])).toContain('No workload of this namespace changed state')
    expect(evidence['workloads:research'].map(e => e.source)).toEqual([WORKLOADS, COVERAGE])
    const db = JSON.stringify(databasePanels(s.databases, evidence))
    expect(db).toContain('"label":"Backup","value":"ok"')
    expect(db).toContain('2026-11-23T04:48:34Z')
    expect(db).toContain('not a fresh probe')
  })
  it('keeps a failed read unknown with its reason, and the other source standing', async () => {
    const s = await readStates(async () => { throw new Error('ledger read failed') })
    const c = await readCoverage(async () => readout)
    const evidence = { ...s.evidence, ...c.evidence }
    const panels = workloadPanels(s.workloads, c.readout, evidence)
    expect(JSON.stringify(panels)).toContain('Workload states unknown · Error: ledger read failed')
    expect(JSON.stringify(panels)).toContain('20%')
    expect(JSON.stringify(databasePanels(s.databases, evidence))).toContain('Unknown · Error: ledger read failed')
    expect(evidence['@workloads'][0].outcome).toBe('error')
  })
})

describe('Argo CD station', () => {
  // 2026-11-23: Argo sync state was on the Releases page only; Signals had no GitOps reading.
  it('lists each app as Argo reports it and keeps a failed read unknown', async () => {
    const ok = await readStates(async p => p === GITOPS ? { argocd_status: 'installed', reachability: 'ok', detail: '', apps: [{ name: 'bifrost-prod', namespace: 'cicd', sync_status: 'OutOfSync', health_status: 'Healthy', automated_sync: false }], generated_at: '2026-11-23T15:00:00Z' } : { states: [] })
    const text = JSON.stringify(gitopsPanel(ok.gitops, ok.evidence))
    expect(text).toContain('"text":"OutOfSync · Healthy"')
    expect(text).toContain('automated sync off')
    expect(ok.evidence['@gitops'][0].evaluatedAt).toBe('2026-11-23T15:00:00Z')
    const bad = await readStates(async () => { throw new Error('forbidden') })
    expect(JSON.stringify(gitopsPanel(bad.gitops, bad.evidence))).toContain('Unknown · Error: forbidden')
  })
})
