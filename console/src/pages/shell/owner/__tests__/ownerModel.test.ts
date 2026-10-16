import { describe, expect, it } from 'vitest'
import progressFixture from '@/api/__fixtures__/agentrt-progress.stg.json'
import roadmap from '@/api/__fixtures__/board-roadmap-round-arch.json'
import { parseRuntimeProgress } from '@/api/agentRuntime'
import { parseRuntimeRound } from '@/api/agentRuntimePlan'
import type { TelemetryAlertEntry, TelemetryTargetEntry } from '@/api/clusterTypes'
import { backupTile, copiesTile, marketTile, pluginsTile, researchTile } from '../dataTiles'
import { ago, dataBadge, ownerVerdict, planRows, quotaRows, splitAlerts, waitingRow, workNow, yoursCount, QUOTA_STALE_MS } from '../ownerModel'
import { parseDaemonSetGaps, parseDownTargets, parseStandbyPods, standbyPodsQuery, type StandbyEvidence } from '../standbyEvidence'
import { defaultOwnerTab, ownerBlocks, ownerTabFromHash, ownerTabHash } from '../ownerTabs'
import alertsFixture from './fixtures/telemetry-alerts.prod.json'
import evidenceFixture from './fixtures/standby-evidence.prod.json'

const alerts = alertsFixture.alerts as TelemetryAlertEntry[]
const NOW = Date.parse('2026-10-16T20:10:43Z')
const STANDBY = [{ name: 'gpu-server', internalIp: '192.168.10.60' }]

const ev: StandbyEvidence = {
  nodes: STANDBY,
  pods: parseStandbyPods(evidenceFixture.pods),
  gaps: parseDaemonSetGaps(evidenceFixture.gaps),
  down: parseDownTargets(evidenceFixture.targets as TelemetryTargetEntry[]),
}
const alert = (labels: Record<string, string>, value = '1') => ({ labels: { severity: 'warning', ...labels }, annotations: {}, state: 'firing', active_at: '2026-10-16T10:00:00Z', value })

describe('alerts on the Owner Console (PROD, 2026-10-16 15:10 CDT)', () => {
  const split = splitAlerts(alerts, ev, NOW)

  it('makes the TWS login the Owner\'s, and tells the IB plugin with it', () => {
    expect(split.yours.map(r => r.title)).toEqual(['Log in to TWS for the IB secondary slot'])
    expect(split.yours[0].tone).toBe('fail')
    expect(split.yours[0].detail).toContain('Only you hold the TWS login')
    expect(split.merged).toBe(1)
  })

  it('lists the four real warnings, oldest first, each with no goal yet and a goal draft', () => {
    expect(split.worth.map(r => r.title)).toEqual([
      'kube-prometheus-stack-grafana targets down',
      'Job logical-backup-drill-manual-1 failed',
      'PostgreSQL connection count high',
      'Bifrost API high P99 latency',
    ])
    expect(split.worth.every(r => r.whose === 'No goal yet' && r.goalDraft?.includes('fix the cause'))).toBe(true)
    expect(split.worth[3].detail).toBe('warning · bifrost-platform-prod · since 15 h ago')
  })

  it('puts the 19 standby, info and watchdog alerts in noise, each on the cluster\'s evidence', () => {
    expect(split.noise).toBe(19)
    expect(split.critical).toBe(1)
    expect(split.warning).toBe(4)
    expect(split.noise + split.merged + split.yours.length + split.worth.length).toBe(alerts.filter(a => a.state === 'firing').length)
  })

  it('without the evidence, every standby alert stays real', () => {
    const none = splitAlerts(alerts, null, NOW)
    expect(none.noise).toBe(2) // the watchdog and the standby marker (info)
    expect(none.worth.map(r => r.title)).toContain('Node is unreachable.')
    expect(none.worth.map(r => r.title)).toContain('Pod promtail-7fxmt not ready')
  })

  it('a target down on an active pod is real, whatever its share (review 221 A2)', () => {
    const app = { ...ev, down: [...ev.down, { job: 'my-app', instance: '10.42.3.9:8080', node: '', namespace: 'apps', pod: 'my-app-1' }] }
    expect(splitAlerts([alert({ alertname: 'TargetDown', job: 'my-app', namespace: 'apps' }, '10')], app, NOW).worth).toHaveLength(1)
    const activeNode = { ...ev, down: [...ev.down, { job: 'node-exporter', instance: '192.168.10.70:9100', node: '', namespace: 'monitoring', pod: '' }] }
    expect(splitAlerts([alert({ alertname: 'TargetDown', job: 'node-exporter', namespace: 'monitoring' }, '16.7')], activeNode, NOW).worth).toHaveLength(1)
  })

  it('a pod or DaemonSet on an active node is real, whatever its name (review 221 A3)', () => {
    const pod = alert({ alertname: 'KubePodNotReady', namespace: 'monitoring', pod: 'node-reboot-required-zz9', container: 'kube-state-metrics', job: 'kube-state-metrics' })
    expect(splitAlerts([pod], ev, NOW).worth).toHaveLength(1)
    const twoDown = { ...ev, gaps: ev.gaps.map(g => g.daemonset === 'node-reboot-required' ? { ...g, gap: 2 } : g) }
    const ds = alert({ alertname: 'KubeDaemonSetRolloutStuck', namespace: 'monitoring', daemonset: 'node-reboot-required', container: 'kube-state-metrics', job: 'kube-state-metrics' })
    expect(splitAlerts([ds], ev, NOW).noise).toBe(1)
    expect(splitAlerts([ds], twoDown, NOW).worth).toHaveLength(1)
  })

  it('a pod of the same name in another namespace is real (review 221 B1)', () => {
    const other = alert({ alertname: 'KubePodNotReady', namespace: 'apps', pod: 'promtail-7fxmt', container: 'kube-state-metrics', job: 'kube-state-metrics' })
    expect(splitAlerts([other], ev, NOW).worth).toHaveLength(1)
  })

  it('reads kube-state-metrics\' own pod label as the exporter, not the subject', () => {
    const job = alert({ alertname: 'KubeJobFailed', namespace: 'data', job_name: 'x', pod: 'kube-prometheus-stack-kube-state-metrics-647cd7788b-jsmcl', container: 'kube-state-metrics' })
    const withExporter = { ...ev, pods: [...ev.pods, { namespace: 'data', pod: 'kube-prometheus-stack-kube-state-metrics-647cd7788b-jsmcl', ip: '', daemonset: '' }] }
    expect(splitAlerts([job], withExporter, NOW).worth).toHaveLength(1)
  })

  it('ignores a pending alert, like the shell\'s verdict', () => {
    expect(splitAlerts([{ ...alert({ alertname: 'X', severity: 'critical' }), state: 'pending' }], ev, NOW)).toMatchObject({ critical: 0, noise: 0, worth: [] })
  })

  it('asks for the standby node\'s pods by name', () => {
    expect(standbyPodsQuery(STANDBY)).toBe('kube_pod_info{node=~"gpu-server"}')
  })
})

describe('the verdict', () => {
  const base = { approvals: 0, running: 0, lane: 'release lane free', nodes: { ready: 5, total: 5 } }
  it('says nothing needs him, with the four facts behind it', () => {
    expect(ownerVerdict({ ...base, yours: { state: 'known', count: 0 } })).toEqual({
      tone: 'ok', headline: 'Nothing needs you', line: 'Nothing to approve · agents idle · release lane free · 5 of 5 nodes ready' })
  })
  it('counts his alerts with his waiting items', () => {
    const v = ownerVerdict({ ...base, approvals: 1, running: 2, yours: yoursCount({ state: 'known', count: 1 }, { state: 'known', count: 1 }) })
    expect(v.headline).toBe('2 things are yours')
    expect(v.tone).toBe('attention')
    expect(v.line).toBe('1 request to approve · 2 agents running · release lane free · 5 of 5 nodes ready')
  })
  it('alerts not read, or still loading, never add up to nothing (review 221 A1)', () => {
    const unread = yoursCount({ state: 'known', count: 0 }, { state: 'unknown', reason: 'Alerts could not be read: HTTP 503' })
    expect(unread).toEqual({ state: 'unknown', reason: 'Alerts could not be read: HTTP 503' })
    expect(ownerVerdict({ ...base, yours: unread })).toMatchObject({ tone: 'unknown', headline: 'Could not read everything waiting for you' })
    expect(yoursCount({ state: 'known', count: 0 }, { state: 'loading' })).toEqual({ state: 'loading' })
    expect(ownerVerdict({ ...base, yours: { state: 'loading' } }).headline).toBe('Checking what is yours…')
  })
  it('says what it could not read in the facts too', () => {
    const v = ownerVerdict({ ...base, approvals: null, running: null, nodes: null, yours: { state: 'unknown', reason: 'questions request failed' } })
    expect(v.line).toBe('questions request failed · approvals unknown · agents unknown · release lane free · nodes unknown')
  })
  it('marks a coordinator item as kept by hand', () => {
    expect(waitingRow({ id: 'c', title: 'Look', href: '#progress', instruction: 'Open Progress', coordinator: {} }).hand).toBe(true)
    expect(waitingRow({ id: 'q', title: 'Q1', href: '#needs-you', instruction: 'Answer' }).hand).toBe(false)
  })
})

describe('work, plan and quota', () => {
  it('reads running, stuck, open goals and the last finished goal from the runtime', () => {
    const runtime = parseRuntimeProgress(progressFixture)
    const work = workNow(runtime)!
    expect(work.running).toBe(runtime.live.length)
    expect(work.open).toBe(runtime.work.missions.length)
    expect(work.lastFinished?.mission).toMatch(/^M-\d+$/)
    expect(workNow(undefined)).toBeNull()
  })
  it('tags the first step not started as next', () => {
    const rows = planRows(parseRuntimeRound(roadmap.round))
    expect(rows.map(r => r.key)).toEqual(['2.0', '2.1', '2.2', '2.2b', '2.3', '2.4', '2.5'])
    expect(rows.find(r => r.tag === 'next')?.key).toBe('2.3')
    expect(rows[0]).toMatchObject({ tone: 'ok', done: 3, total: 3 })
    expect(rows[1].tone).toBe('info')
  })
  it('greys a quota read more than a day ago, never red, and leaves out sub-limits', () => {
    const fresh = (NOW - 3600_000) / 1000, old = (NOW - QUOTA_STALE_MS - 1000) / 1000
    const rows = quotaRows([
      { vendor: 'Codex', label: 'weekly', used: 6, read_at: fresh },
      { vendor: 'Claude', label: 'weekly', used: 91, read_at: old },
      { vendor: 'Claude', label: 'Fable', used: 0, read_at: fresh, sublimit: true },
      { vendor: 'Cursor', label: 'other', used: 95, read_at: fresh },
    ], NOW)
    expect(rows.map(r => [r.name, r.stale, r.tone])).toEqual([
      ['Codex · weekly', false, 'ok'], ['Claude · weekly', true, 'muted'], ['Cursor · other', false, 'fail']])
  })
})

describe('data tiles', () => {
  it('says unknown, never OK, when a source cannot be read', () => {
    expect(backupTile({ data: undefined, error: 'HTTP 503' }, NOW)).toMatchObject({ tone: 'unknown', state: 'Unknown', detail: 'HTTP 503' })
    expect(marketTile({ data: undefined, error: 'plugin did not answer' }, '2026-10-16').tone).toBe('unknown')
  })
  it('has no session of its own: an unreadable session is Unknown, not today in UTC (review 221 A5)', () => {
    expect(marketTile({ data: [{ last_run_at: '2026-10-16T21:00:00Z' }], error: null }, null)).toMatchObject({ tone: 'unknown', state: 'Unknown' })
    expect(marketTile({ data: [{ last_run_at: '2026-10-16T21:00:00Z' }], error: null }, undefined)).toMatchObject({ state: 'Loading' })
  })
  it('counts the session\'s feeds that ran', () => {
    const tile = marketTile({ data: [{ last_run_at: '2026-10-16T21:00:00Z' }, { last_run_at: '2026-10-15T21:00:00Z' }], error: null }, '2026-10-16')
    expect(tile).toMatchObject({ figure: '1/2 for session', state: 'Partly in', tone: 'warn' })
  })
  it('names the degraded plugin first', () => {
    const tile = pluginsTile([{ name: 'IB', reach: 'degraded', loading: false }, { name: 'Massive', reach: 'ok', loading: false }, { name: 'IB Flex', reach: 'ok', loading: false }])
    expect(tile).toMatchObject({ figure: 'IB degraded', state: 'Degraded', detail: 'Massive OK · IB Flex OK' })
  })
  it('reads the research batch and the backups', () => {
    expect(researchTile({ data: { verdict: 'healthy', job_name: 'trading_day', overdue: false, detail: '', schedules_total: 39, schedules_running: 39,
      last_run_status: 'SUCCESS', last_run_ended_at: '2026-10-16T02:50:00Z' }, error: null }, NOW)).toMatchObject({ tone: 'ok', figure: '39/39 schedules', detail: 'last run SUCCESS 17 h ago' })
    expect(backupTile({ data: { fresh: true, signal: 'ok', detail: '', max_age_hours: 26, backup_count: 9, last_completed_at: '2026-10-16T04:27:00Z', generated_at: '' }, error: null }, NOW))
      .toMatchObject({ tone: 'ok', figure: '15 h ago' })
  })
  it('takes the oldest test copy against the thresholds', () => {
    const tile = copiesTile({ data: { cluster_id: '', reference_db: 'bifrost_prod', cache_hit: false, generated_at: '', fresh_threshold_days: 3, stale_threshold_days: 7,
      databases: [{ name: 'bifrost_prod', environment: 'prod', verdict: 'reference' }, { name: 'bifrost_dev', environment: 'dev', verdict: 'stale', lag_vs_prod_days: 13.2 },
        { name: 'bifrost_stg', environment: 'stg', verdict: 'stale', lag_vs_prod_days: 12.8 }] }, error: null })
    expect(tile).toMatchObject({ tone: 'warn', state: 'Stale', figure: '13 days', detail: 'dev and stg copies of PROD, oldest' })
    const partly = copiesTile({ data: { cluster_id: '', reference_db: 'bifrost_prod', cache_hit: false, generated_at: '', fresh_threshold_days: 3, stale_threshold_days: 7,
      databases: [{ name: 'bifrost_dev', environment: 'dev', verdict: 'fresh', lag_vs_prod_days: 0 }, { name: 'bifrost_stg', environment: 'stg', verdict: 'unknown' }] }, error: null })
    expect(partly).toMatchObject({ tone: 'unknown', state: 'Unknown', detail: 'stg not read' })
    expect(dataBadge([tile, { ...tile, tone: 'ok' }, { ...tile, tone: 'muted' }, { ...tile, tone: 'unknown' }])).toBe(2)
  })
  it('writes ages in minutes, hours, then days', () => {
    expect([ago(NOW - 30_000, NOW), ago(NOW - 5 * 60_000, NOW), ago(NOW - 15 * 3600_000, NOW), ago(NOW - 13 * 86_400_000, NOW), ago('', NOW)])
      .toEqual(['just now', '5 min ago', '15 h ago', '13 days ago', 'time unknown'])
  })
})

describe('tabs', () => {
  it('opens a phone on For you and anything wider on All', () => {
    expect(defaultOwnerTab(true)).toBe('you')
    expect(defaultOwnerTab(false)).toBe('all')
  })
  it('keeps the tab in the hash and ignores an unknown one', () => {
    expect(ownerTabHash('work')).toBe('#owner?tab=work')
    expect(ownerTabFromHash('#owner?tab=system')).toBe('system')
    expect(ownerTabFromHash('#owner?tab=nope')).toBeNull()
    expect(ownerTabFromHash('#owner')).toBeNull()
  })
  it('All shows every block; a tab shows its own', () => {
    expect(Object.values(ownerBlocks('all')).every(Boolean)).toBe(true)
    expect(ownerBlocks('data')).toEqual({ you: false, chief: false, work: false, system: false, data: true })
  })
})
