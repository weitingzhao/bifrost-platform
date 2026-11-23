import { describe, it, expect } from 'vitest'
import { COVERAGE, DATABASES, GITOPS, WORKLOADS, databasePanels, gitopsPanel, readCoverage, readStates, workloadPanels } from '../states'
import { plainReason } from '../plain'

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
    // 2026-11-23 (箭10, brand layer item 5): the tag is the Ops word, the bus's own word is in the sentence.
    expect(db).toContain('"label":"Backup","value":{"text":"ok","tone":"ok"')
    expect(db).toContain('Bus says ready ready, backup ok, archiving ok')
    expect(db).toContain('2026-11-23T04:48:34Z')
    expect(db).toContain('not a fresh probe')
  })
  it('keeps a failed read unknown with its reason, and the other source standing', async () => {
    const s = await readStates(async () => { throw new Error('ledger read failed') })
    const c = await readCoverage(async () => readout)
    const evidence = { ...s.evidence, ...c.evidence }
    const panels = workloadPanels(s.workloads, c.readout, evidence)
    expect(JSON.stringify(panels)).toContain('Workload states unknown — the read failed.')
    expect(JSON.stringify(panels)).toContain('20%')
    expect(JSON.stringify(databasePanels(s.databases, evidence))).toContain('Unknown — the read failed.')
    expect(evidence['@workloads'][0].outcome).toBe('error')
  })
})

describe('Argo CD station', () => {
  // 2026-11-23: Argo sync state was on the Releases page only; Signals had no GitOps reading.
  it('lists each app as Argo reports it and keeps a failed read unknown', async () => {
    const ok = await readStates(async p => p === GITOPS ? { argocd_status: 'installed', reachability: 'ok', detail: '', apps: [{ name: 'bifrost-prod', namespace: 'cicd', sync_status: 'OutOfSync', health_status: 'Healthy', automated_sync: false }], generated_at: '2026-11-23T15:00:00Z' } : { states: [] })
    const text = JSON.stringify(gitopsPanel(ok.gitops, ok.evidence))
    // 箭10 2026-11-23: OutOfSync is warn (look at it); Argo's words stay in the sentence.
    expect(text).toContain('"label":"bifrost-prod","value":{"text":"warn","tone":"warn"')
    expect(text).toContain('Argo says: bifrost-prod OutOfSync · Healthy')
    expect(text).toContain('automated sync off')
    expect(ok.evidence['@gitops'][0].evaluatedAt).toBe('2026-11-23T15:00:00Z')
    const bad = await readStates(async () => { throw new Error('forbidden') })
    expect(JSON.stringify(gitopsPanel(bad.gitops, bad.evidence))).toContain('Unknown — this viewer may not read it.')
  })
})

describe('plain failure reasons', () => {
  // 2026-11-23 (箭10): every domain card showed "SyntaxError: Unexpected token '<'". The card now says why in
  // plain words; the source's text stays in the evidence.
  it('classifies the failure and keeps the raw text in evidence', async () => {
    expect(plainReason("SyntaxError: Unexpected token '<', \"<!doctype \"... is not valid JSON")).toBe('the source did not answer with data')
    expect(plainReason('Error: Signal source: viewer token required (401)')).toBe('needs a signed-in viewer')
    expect(plainReason('TypeError: Failed to fetch')).toBe('the source could not be reached')
    expect(plainReason('something new')).toBe('the read failed')
    const s = await readStates(async () => { throw new Error('viewer token required') })
    expect(JSON.stringify(databasePanels(s.databases, s.evidence))).not.toContain('viewer token required')
    expect(s.evidence['@databases'][0].detail).toBe('Error: viewer token required')
  })
})

describe('Argo words the engine table does not hold', () => {
  // 箭10 2026-11-23: Progressing is info (activity, not a judgement), OutOfSync warn; nothing else guessed.
  it('maps Progressing to info and keeps Healthy ok', async () => {
    const r = await readStates(async p => p === GITOPS ? { argocd_status: 'installed', reachability: 'ok', detail: '', generated_at: '2026-11-23T15:00:00Z', apps: [
      { name: 'research', namespace: 'cicd', sync_status: 'Synced', health_status: 'Progressing' },
      { name: 'stg-platform', namespace: 'cicd', sync_status: 'Synced', health_status: 'Healthy' },
    ] } : { states: [] })
    const text = JSON.stringify(gitopsPanel(r.gitops, r.evidence))
    expect(text).toContain('"label":"research","value":{"text":"info","tone":"info"')
    expect(text).toContain('"label":"stg-platform","value":{"text":"ok","tone":"ok"')
  })
})
