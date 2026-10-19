import { describe, expect, it } from 'vitest'
import progressFixture from '@/api/__fixtures__/agentrt-progress.stg.json'
import roadmap from '@/api/__fixtures__/board-roadmap-round-arch.json'
import etaFixture from '@/api/__fixtures__/agentrt-progress-eta.testdb.json'
import { parseMissionEta, parseRuntimeProgress } from '@/api/agentRuntime'
import { parsePools } from '@/api/agentRuntimePools'
import { parseRuntimeRound } from '@/api/agentRuntimePlan'
import type { TelemetryAlertEntry, TelemetryTargetEntry } from '@/api/clusterTypes'
import { backupTile, copiesTile, marketTile, pluginsTile, researchTile } from '../dataTiles'
import { ago, clockTime, etaBasis, etaLine, yoursNavBadge, goalRefreshMs, lastResult, lastResultLine, ownerVerdict, planRows, poolRows, splitAlerts, waitingRow, workNow, yoursCount } from '../ownerModel'
import { DAEMONSET_GAPS_QUERY, parseDaemonSetGaps, parseDownTargets, parseStandbyPods, standbyPodsQuery, type StandbyEvidence } from '../standbyEvidence'
import alertsFixture from './fixtures/telemetry-alerts.prod.json'
import evidenceFixture from './fixtures/standby-evidence.prod.json'
import poolsFixture from './fixtures/pools-utc.json'

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

  it('lists the four real warnings, oldest first, and the standby-shaped DaemonSet alerts as one row', () => {
    expect(split.worth.map(r => r.title)).toEqual([
      'kube-prometheus-stack-grafana targets down',
      'Job logical-backup-drill-manual-1 failed',
      '6 DaemonSet alerts that fit gpu-server being on standby',
      'PostgreSQL connection count high',
      'Bifrost API high P99 latency',
    ])
    expect(split.worth.filter(r => r.whose === 'No goal yet').length).toBe(4)
    expect(split.worth.every(r => r.goalDraft?.includes('fix the cause'))).toBe(true)
    expect(split.worth[2]).toMatchObject({ whose: 'Likely the planned standby; not proven',
      detail: 'kube-prometheus-stack-prometheus-node-exporter, node-reboot-required, promtail, svclb-traefik-8df577f9 · since 6 days ago' })
    expect(split.worth[4].detail).toBe('warning · bifrost-platform-prod · since 15 h ago')
  })

  it('puts the 13 proven standby, info and watchdog alerts in noise; the DaemonSet ones stay counted', () => {
    expect(split.noise).toBe(13)
    expect(split.critical).toBe(1)
    expect(split.warning).toBe(10)
    // Every firing alert is counted once: noise, told with another, or critical / warning (the DaemonSet row stands for six).
    expect(split.noise + split.merged + split.critical + split.warning).toBe(alerts.filter(a => a.state === 'firing').length)
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
    expect(splitAlerts([ds], twoDown, NOW).worth.map(r => r.title)).toEqual(['DaemonSet node-reboot-required rollout stuck'])
  })

  it('a DaemonSet short by one pod is never hidden: the count fits, but not which pod (review 222 A-R1)', () => {
    const ds = alert({ alertname: 'KubeDaemonSetRolloutStuck', namespace: 'monitoring', daemonset: 'node-reboot-required', container: 'kube-state-metrics', job: 'kube-state-metrics' })
    const split1 = splitAlerts([ds], ev, NOW)
    expect(split1.noise).toBe(0)
    expect(split1.worth).toHaveLength(1)
    expect(split1.worth[0].whose).toBe('Likely the planned standby; not proven')
    expect(split1.warning).toBe(1)
  })

  it('adds a DaemonSet\'s unavailable and mis-scheduled pods (review 222 A-R1)', () => {
    expect(DAEMONSET_GAPS_QUERY).toBe('kube_daemonset_status_number_unavailable + kube_daemonset_status_number_misscheduled > 0')
  })

  it('a pod named in another namespace stays real even when its address is a standby pod\'s (review 222 A-R2)', () => {
    const crash = alert({ alertname: 'KubePodCrashLooping', namespace: 'apps', pod: 'other-pod', instance: '10.42.4.163:8080' })
    expect(splitAlerts([crash], ev, NOW).worth).toHaveLength(1)
  })

  it.each(['KubeNodeNotReady', 'KubeNodeUnreachable', 'KubeletInstanceUnreachable'])('%s on a named active node is real even at a standby address (review 223 A-223-1)', name => {
    const split = splitAlerts([alert({ alertname: name, node: 'ubt-k3s-02', instance: '192.168.10.60:10250' })], ev, NOW)
    expect(split).toMatchObject({ noise: 0, warning: 1 })
    expect(split.worth).toHaveLength(1)
    expect(splitAlerts([alert({ alertname: name, instance: '192.168.10.60:10250' })], ev, NOW).noise).toBe(1)
  })

  it('a standby pod that also names its standby node stays noise (review 223 B-223-1)', () => {
    const pod = alert({ alertname: 'KubePodNotReady', namespace: 'monitoring', pod: 'promtail-7fxmt', node: 'gpu-server', container: 'kube-state-metrics', job: 'kube-state-metrics' })
    expect(splitAlerts([pod], ev, NOW).noise).toBe(1)
    const otherPod = alert({ alertname: 'KubePodNotReady', namespace: 'monitoring', pod: 'promtail-zz', node: 'gpu-server', container: 'kube-state-metrics', job: 'kube-state-metrics' })
    expect(splitAlerts([otherPod], ev, NOW).worth).toHaveLength(1)
  })

  it('a named active node wins over a cached standby pod of the same name (review 222 A-R2)', () => {
    const moved = alert({ alertname: 'KubePodNotReady', namespace: 'monitoring', pod: 'promtail-7fxmt', node: 'ubt-k3s-02', container: 'kube-state-metrics', job: 'kube-state-metrics' })
    expect(splitAlerts([moved], ev, NOW).worth).toHaveLength(1)
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
  it('makes a waiting item a row with its way in', () => {
    expect(waitingRow({ id: 'q', title: 'Q1', href: '#needs-you', instruction: 'Answer', since: '2026-10-19T12:00:00Z' }))
      .toEqual({ id: 'q', title: 'Q1', detail: 'Answer', tone: 'attention', href: '#needs-you', action: 'Open', since: '2026-10-19T12:00:00Z' })
  })
})

describe('work, plan and quota', () => {
  it('reads running, stuck, open goals and the latest accepted implementation from the runtime', () => {
    const runtime = parseRuntimeProgress(progressFixture)
    const work = workNow(runtime)!
    expect(work.running).toBe(runtime.live.length)
    expect(work.open).toBe(runtime.work.missions.length)
    expect(work.lastAccepted?.mission).toMatch(/^M-\d+$/)
    expect(workNow(undefined)).toBeNull()
  })
  it('calls a goal done only when its mission is done; an accepted implementation is not a done goal (review 224 A-224-1)', () => {
    const work = { running: 0, stuck: 1, open: 1, lastAccepted: { mission: 'M-7', at: new Date(NOW - 2 * 3600_000).toISOString(), openState: 'paused' } }
    expect(lastResult(work, '')).toMatchObject({ status: 'open', state: 'paused' })
    expect(lastResultLine(lastResult(work, '')!, NOW)).toBe('M-7 implementation accepted 2 h ago; the goal is paused')
    expect(lastResult(work, 'active')).toMatchObject({ status: 'open', state: 'active' })
    expect(lastResultLine(lastResult(work, 'done')!, NOW)).toBe('M-7 done 2 h ago')
    const gone = { ...work, lastAccepted: { ...work.lastAccepted, openState: '' } }
    expect(lastResultLine(lastResult(gone, '')!, NOW)).toBe('M-7 implementation accepted 2 h ago')
    expect(lastResult(null, 'done')).toBeNull()
  })

  it('reads a goal that is not done again, and stops once it is (review 225 B-225-1)', () => {
    expect(goalRefreshMs('active')).toBe(30_000)
    expect(goalRefreshMs('paused')).toBe(30_000)
    expect(goalRefreshMs('')).toBe(30_000)
    expect(goalRefreshMs('done')).toBe(false)
  })

  it('names the subject, not kube-state-metrics\' own pod, as where an alert is', () => {
    const job = { labels: { alertname: 'KubeJobFailed', severity: 'warning', namespace: 'data', job_name: 'drill', pod: 'kube-state-metrics-x', container: 'kube-state-metrics' }, annotations: {}, state: 'firing', active_at: '2026-10-16T10:00:00Z' }
    expect(splitAlerts([job], null, NOW).worth[0].detail).toBe('warning · data · since 10 h ago')
  })

  it('tags the first step not started as next', () => {
    const rows = planRows(parseRuntimeRound(roadmap.round))
    expect(rows.map(r => r.key)).toEqual(['2.0', '2.1', '2.2', '2.2b', '2.3', '2.4', '2.5'])
    expect(rows.find(r => r.tag === 'next')?.key).toBe('2.3')
    expect(rows[0]).toMatchObject({ tone: 'ok', done: 3, total: 3 })
    expect(rows[1].tone).toBe('info')
  })
  // GET /agentrt/pools as the platform answered (pools-utc.json, review 281), read a day and an hour later.
  it('gives each pool its last reading and when, and marks a reading old once its window reset or a day went by (2.5 G7 A)', () => {
    const pools = parsePools(poolsFixture)
    const at = Date.parse('2026-10-18T20:30:00Z')
    expect(poolRows(pools, at).map(r => [r.id, r.used, r.old, r.tone, r.cooling])).toEqual([
      ['claude:owner:short', 100, '', 'fail', true],
      ['claude:owner:weekly', 20, '', 'ok', false],
      ['codex:owner:weekly', 50, '', 'ok', false],
      // A refusal without a figure: nothing read, so nothing old; it cools.
      ['cursor:owner:monthly', null, '', 'fail', true],
    ])
    const later = Date.parse('2026-10-19T21:00:00Z')
    expect(poolRows(pools, later).map(r => [r.name, r.old, r.tone])).toEqual([
      ['claude · short', 'its window has reset since it was read', 'fail'],
      ['claude · weekly', 'read more than a day ago', 'muted'],
      ['codex · weekly', 'read more than a day ago', 'muted'],
      ['cursor · monthly', '', 'fail'],
    ])
    // A short window's reading is old after its five hours, before a day.
    const short = { ...pools[0], resets_at: null, cooling: false, cooldown_until: null, used_fraction: 0.3 }
    expect(poolRows([short], Date.parse(short.measured_at!) + 5 * 3600_000 + 1000)[0]).toMatchObject({ old: 'read more than 5 hours ago', tone: 'muted' })
    expect(poolRows([short], Date.parse(short.measured_at!) + 4 * 3600_000)[0]).toMatchObject({ old: '', tone: 'ok', name: 'claude · short' })
  })
})

// GET /agentrt/progress from the handler on a test database (store's kernel world: three accepted
// implementations of 100 s, two queued): M-1 is estimated, M-2 waits for a decision.
describe('when a goal should be done (2.5 G8)', () => {
  const [m1, m2] = parseRuntimeProgress(etaFixture).work.missions
  const generated = Date.parse('2026-10-19T23:35:51Z')
  it('reads the estimate as a range with its basis, and an unknown one with why', () => {
    expect(m1.eta).toEqual({ state: 'estimated', earliest: '2026-10-19T23:39:11Z', latest: '2026-10-19T23:40:51Z', past_usual: [],
      basis: [{ role: 'implement', task_class: 'go', usual_s: 100, samples: 3 }], reason: etaFixture.work.missions[0].eta.reason })
    expect(m2.eta).toEqual({ state: 'unknown', reason: 'the mission is proposed: a wait for a decision has no usual time' })
  })
  it('says the range in local times and how far off, never a fixed unknown', () => {
    const lo = Date.parse('2026-10-19T23:39:11Z'), hi = Date.parse('2026-10-19T23:40:51Z')
    expect(etaLine(m1.eta, generated)).toBe(`Expected ${clockTime(lo, generated)}–${clockTime(hi, generated)}, in 3 min to 5 min`)
    expect(etaLine(m1.eta, lo + 1000)).toBe(`Expected by ${clockTime(hi, lo)}, within 2 min`)
    expect(etaLine(m1.eta, hi + 1000)).toBe(`Expected by ${clockTime(hi, hi)}; that time has passed`)
    expect(etaLine(m2.eta, generated)).toBe('Done by: unknown — the mission is proposed: a wait for a decision has no usual time')
    expect(etaBasis(m1.eta)).toEqual(['implement go: usually 1 min 40 s, from 3 accepted attempts in 30 days'])
    expect(etaBasis(m2.eta)).toEqual([])
  })
  it('names tasks past their usual time, and takes an estimate it cannot read as unknown', () => {
    const late = parseMissionEta({ ...etaFixture.work.missions[0].eta, past_usual: ['t_1'] })
    expect(etaBasis(late)[1]).toBe('1 task already past 1.5 times its usual time: t_1')
    for (const broken of [{ ...etaFixture.work.missions[0].eta, latest: 'soon' }, { ...etaFixture.work.missions[0].eta, basis: [] },
      { ...etaFixture.work.missions[0].eta, earliest: '2026-10-19T23:41:00Z' }, false, null, { state: 'estimated' }]) {
      expect(parseMissionEta(broken)).toEqual({ state: 'unknown', reason: expect.any(String) })
    }
    expect(parseMissionEta({})).toEqual({ state: 'unknown', reason: 'the runtime gave no estimate' })
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
  })
  it('writes ages in minutes, hours, then days', () => {
    expect([ago(NOW - 30_000, NOW), ago(NOW - 5 * 60_000, NOW), ago(NOW - 15 * 3600_000, NOW), ago(NOW - 13 * 86_400_000, NOW), ago('', NOW)])
      .toEqual(['just now', '5 min ago', '15 h ago', '13 days ago', 'time unknown'])
  })
})

describe('the one count', () => {
  it('shows what is yours, nothing while loading or at zero, and "?" when a source could not be read', () => {
    expect(yoursNavBadge({ state: 'known', count: 2 })).toEqual({ text: '2', title: 'Yours: 2', tone: 'attention' })
    expect(yoursNavBadge({ state: 'known', count: 120 })?.text).toBe('99+')
    expect(yoursNavBadge({ state: 'known', count: 0 })).toBeUndefined()
    expect(yoursNavBadge({ state: 'loading' })).toBeUndefined()
    expect(yoursNavBadge({ state: 'unknown', reason: 'Alerts could not be read: 502' }))
      .toEqual({ text: '?', title: 'Yours: unknown — Alerts could not be read: 502', tone: 'unknown' })
  })
})
