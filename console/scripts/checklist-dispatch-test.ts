/**
 * Checklist dispatch gate + Action progress unit test.
 * Run: npx tsx scripts/checklist-dispatch-test.ts
 */
import assert from 'node:assert/strict'
import {
  buildChecklistFixActions,
  gateForFixCapability,
} from '../src/lib/control-room/checklistDispatch.ts'
import {
  deriveChecklistHeaderProgress,
  deriveChecklistItemProgress,
  fleetAgentSignalDisagree,
  formatDispatchHeaderStrip,
  isBusyQueueDemote,
  resolveSkipKind,
  skipProgressLabel,
} from '../src/lib/control-room/checklistProgress.ts'
import {
  buildChecklistCursorFailoverPack,
  buildChecklistCursorFailoverPrompt,
  buildChecklistItemPlatformFixPrompt,
  checklistItemNeedsAttention,
  checklistItemPlatformFixAllowed,
} from '../src/lib/control-room/checklistCursorFailoverPrompt.ts'
import type { RemediationJob } from '../src/api/types.ts'

let passed = 0
function check(name: string, fn: () => void) {
  try {
    fn()
    passed += 1
    console.log(`ok  ${name}`)
  } catch (err) {
    console.error(`FAIL ${name}`)
    throw err
  }
}

check('gate mapping', () => {
  assert.equal(gateForFixCapability('full_auto'), 'auto')
  assert.equal(gateForFixCapability('semi_auto'), 'queue')
  assert.equal(gateForFixCapability('manual'), 'notify')
  assert.equal(gateForFixCapability('observe'), 'notify')
})

check('D10 skips ib-feed', () => {
  const actions = buildChecklistFixActions([
    { itemId: 'ib-feed', signal: 'fail', detail: 'down' },
    { itemId: 'failing-pods', signal: 'fail', detail: '3' },
  ])
  const ib = actions.find(a => a.itemId === 'ib-feed')
  const pods = actions.find(a => a.itemId === 'failing-pods')
  assert.ok(ib?.skippedD10 && ib.gate === 'skip')
  assert.equal(pods?.gate, 'auto')
})

check('concurrent auto limit demotes to queue', () => {
  const actions = buildChecklistFixActions(
    [
      { itemId: 'failing-pods', signal: 'fail' },
      { itemId: 'redis', signal: 'fail' },
    ],
    { concurrentAutoRemaining: 1 },
  )
  assert.equal(actions.filter(a => a.gate === 'auto').length, 1)
  assert.equal(actions.filter(a => a.gate === 'queue').length, 1)
  const demoted = actions.find(a => a.gate === 'queue')
  assert.ok(demoted?.detail?.toLowerCase().includes('concurrent auto'))
})

check('Phase 4.2 skip labels — dedup vs D10 never imply in-progress', () => {
  assert.equal(skipProgressLabel('dedup'), 'Skip · dedup 24h')
  assert.equal(skipProgressLabel('d10'), 'Skip · D10')
  assert.equal(resolveSkipKind({ item_id: 'x', gate: 'skip', skipped_d10: true }), 'd10')
  assert.equal(
    resolveSkipKind({ item_id: 'x', gate: 'skip', detail: 'dedup: dispatched within last 24h' }),
    'dedup',
  )
  const d10 = deriveChecklistItemProgress({
    dispatch: { item_id: 'ib-feed', gate: 'skip', skipped_d10: true, detail: 'D10' },
  })
  assert.equal(d10.state, 'skip')
  assert.equal(d10.label, 'Skip · D10')
  assert.ok(!d10.label.toLowerCase().includes('remediat'))
  const dedup = deriveChecklistItemProgress({
    dispatch: {
      item_id: 'failing-pods',
      gate: 'skip',
      detail: 'dedup: dispatched within last 24h',
    },
  })
  assert.equal(dedup.label, 'Skip · dedup 24h')
})

check('auto_running + done/failed from linked job', () => {
  const running: RemediationJob = {
    id: 'job-1',
    phase: 'remediating',
    status: 'running',
    scope: 'platform-self-health-recover',
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  }
  const p = deriveChecklistItemProgress({
    dispatch: { item_id: 'platform-api', gate: 'auto', job_id: 'job-1' },
    linkedJob: running,
  })
  assert.equal(p.state, 'auto_running')
  assert.equal(p.label, 'Auto · remediating')
  assert.equal(p.jobId, 'job-1')

  const done = deriveChecklistItemProgress({
    linkedJob: { ...running, status: 'done', phase: 'done' },
  })
  assert.equal(done.state, 'done')
  assert.equal(done.label, 'Done')

  const failed = deriveChecklistItemProgress({
    linkedJob: { ...running, status: 'failed', phase: 'failed', error: 'boom' },
  })
  assert.equal(failed.state, 'failed')
  assert.equal(failed.label, 'Failed')
})

check('header progress — prober checking + dispatch strip', () => {
  const created = new Date(Date.now() - 12_000).toISOString()
  const jobs: RemediationJob[] = [
    {
      id: 'chk-1',
      phase: 'diagnosing',
      status: 'running',
      scope: 'daily-ops-checklist-run',
      created_at: created,
      updated_at: created,
    },
    {
      id: 'fix-1',
      phase: 'remediating',
      status: 'running',
      scope: 'platform-self-health-recover',
      actor: 'checklist-dispatch',
      created_at: created,
      updated_at: created,
    },
  ]
  const header = deriveChecklistHeaderProgress({
    jobs,
    lastDispatch: [
      { item_id: 'platform-api', gate: 'auto', job_id: 'fix-1' },
      { item_id: 'argo-apps', gate: 'queue', detail: 'semi' },
      { item_id: 'ib-feed', gate: 'skip', skipped_d10: true },
    ],
    nowMs: Date.now(),
  })
  assert.equal(header.checking, true)
  assert.ok(header.proberLabel?.includes('Prober:'))
  assert.ok(header.proberElapsedSec != null && header.proberElapsedSec >= 10)
  assert.equal(header.remediating, 1)
  assert.equal(header.dispatchQueued, 1)
  assert.equal(header.dispatchSkip, 1)
  const strip = formatDispatchHeaderStrip(header)
  assert.ok(strip?.includes('queued'))
  assert.ok(strip?.includes('remediat') || strip?.includes('auto'))
})

check('idle item has no fake checking label', () => {
  const idle = deriveChecklistItemProgress({})
  assert.equal(idle.state, 'idle')
  assert.equal(idle.label, '—')
})

check('Wave 4.3 Queued (busy) from demote detail', () => {
  assert.ok(isBusyQueueDemote('concurrent auto limit reached — demoted to Operate Queue'))
  assert.ok(
    isBusyQueueDemote(
      'concurrent auto limit — demoted; enqueued Operate Queue (checklist_dispatch)',
    ),
  )
  assert.equal(isBusyQueueDemote('enqueued Operate Queue (checklist_dispatch)'), false)
  const busy = deriveChecklistItemProgress({
    dispatch: {
      item_id: 'redis',
      gate: 'queue',
      queue_id: 'q-1',
      detail: 'concurrent auto limit reached — demoted to Operate Queue',
    },
  })
  assert.equal(busy.state, 'queued')
  assert.equal(busy.label, 'Queued (busy)')
  assert.equal(busy.openTarget, 'queue')
  assert.equal(busy.busyDemote, true)
  assert.equal(busy.queueId, 'q-1')
})

check('Wave 4.1 queue openTarget + auto job openTarget', () => {
  const q = deriveChecklistItemProgress({
    dispatch: {
      item_id: 'argo-apps',
      gate: 'queue',
      queue_id: 'oq-9',
      detail: 'enqueued Operate Queue (checklist_dispatch)',
    },
  })
  assert.equal(q.label, 'Queued')
  assert.equal(q.openTarget, 'queue')

  const auto = deriveChecklistItemProgress({
    dispatch: { item_id: 'platform-api', gate: 'auto', job_id: 'j-2' },
    linkedJob: {
      id: 'j-2',
      phase: 'verifying',
      status: 'running',
      scope: 'platform-self-health-recover',
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    },
  })
  assert.equal(auto.openTarget, 'job')
  assert.equal(auto.label, 'Auto · verifying')
})

check('Wave 3.1 fleet≠agent polarity only', () => {
  assert.equal(fleetAgentSignalDisagree('ok', 'fail'), true)
  assert.equal(fleetAgentSignalDisagree('fail', 'ok'), true)
  assert.equal(fleetAgentSignalDisagree('degraded', 'ok'), true)
  assert.equal(fleetAgentSignalDisagree('ok', 'ok'), false)
  assert.equal(fleetAgentSignalDisagree('fail', 'degraded'), false)
  assert.equal(fleetAgentSignalDisagree('unknown', 'fail'), false)
  assert.equal(fleetAgentSignalDisagree('ok', 'unknown'), false)
  assert.equal(fleetAgentSignalDisagree('ok', null), false)
})

check('Cursor failover pack + platform Fix gates', () => {
  assert.equal(checklistItemNeedsAttention('fail'), true)
  assert.equal(checklistItemNeedsAttention('ok'), false)
  assert.equal(
    checklistItemPlatformFixAllowed({
      id: 'x',
      label: 'X',
      group: 'seat',
      healthyCriteria: 'ok',
      fixScope: null,
      fixCapability: 'manual',
    }),
    false,
  )
  assert.equal(
    checklistItemPlatformFixAllowed({
      id: 'y',
      label: 'Y',
      group: 'control',
      healthyCriteria: 'ok',
      fixScope: 'operator-plane-remediate',
      fixCapability: 'semi_auto',
    }),
    true,
  )
  assert.equal(
    checklistItemPlatformFixAllowed({
      id: 'ib-feed',
      label: 'IB',
      group: 'feed',
      healthyCriteria: 'ok',
      fixScope: null,
      fixCapability: 'observe',
    }),
    false,
  )

  const sample = {
    stepOrder: 3,
    stepLabel: 'Engineer · Operator Plane',
    item: {
      id: 'mac-probe-bridge',
      label: 'Mac seat · probe-bridge',
      group: 'seat' as const,
      healthyCriteria: 'probe ok',
      fixScope: null,
      fixCapability: 'manual' as const,
      manualAction: 'Power on Mac',
    },
    overallSignal: 'fail' as const,
    matchedStandards: [
      {
        id: 'mac-seat',
        label: 'Mac seat',
        signal: 'fail' as const,
        detail: 'unreachable',
        source: 'probe',
        cellRole: 'engineer',
        cellEnv: null,
      },
    ],
  }
  const single = buildChecklistCursorFailoverPrompt(sample)
  assert.match(single, /failover/)
  assert.match(single, /mac-probe-bridge/)
  assert.match(single, /D10/)
  const pack = buildChecklistCursorFailoverPack([sample])
  assert.match(pack, /Failing checklist items \(1\)/)
  const plat = buildChecklistItemPlatformFixPrompt({
    ...sample,
    item: {
      ...sample.item,
      id: 'runners-ha',
      fixScope: 'operator-plane-remediate',
      fixCapability: 'semi_auto',
    },
  })
  assert.match(plat, /checklist-item-fix/)
  assert.match(plat, /operator-plane-remediate/)
})

console.log(`\n${passed} checks passed`)
