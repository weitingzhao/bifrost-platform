/**
 * Local evidence for Launch Plugin lane (Detect → Live check).
 * Not Tekton — IB: make install-ib-gateway; Market Data: kubectl apply -k overlays/{seat}.
 *
 * Store is keyed by target + seat so switching plugins does not wipe sibling evidence.
 */

export const PLUGIN_LAUNCH_EVIDENCE_KEY = 'bifrost.pluginLaunch.evidence'
export const PLUGIN_LAUNCH_STORE_KEY = 'bifrost.pluginLaunch.store.v2'

export type PluginLaunchTargetId = 'ib-gateway' | 'market-data'
export type PluginLaunchSeat = 'dev' | 'stg' | 'prod'

export type PluginLaunchStepId =
  | 'detect'
  | 'approve'
  | 'install'
  | 'verify'
  | 'live-check'

export type PluginLaunchEvidence = {
  /** Expected / dogfood revision hint (plugin git SHA short or image tag). */
  revisionHint?: string
  lastDetectAt?: string
  lastApproveAt?: string
  approvedBy?: string
  lastInstallAt?: string
  installOutcome?: 'ok' | 'failed' | 'pending'
  lastVerifyAt?: string
  verifyOutcome?: 'ok' | 'failed' | 'pending'
  lastLiveCheckAt?: string
  liveCheckOutcome?: 'ok' | 'failed' | 'pending'
  /** Set when operator starts the next publish cycle after Published. */
  lastPublishedAt?: string
  lastPublishedRevision?: string
  notes?: string
  updatedAt?: string
}

export type PluginLaunchStore = {
  selectedTarget: PluginLaunchTargetId
  selectedSeat: PluginLaunchSeat
  byKey: Record<string, PluginLaunchEvidence>
  updatedAt?: string
}

export const PLUGIN_DOGFOOD_REVISION = 'b2fb081'
export const PLUGIN_DOGFOOD_FEATURE = 'on-demand STK'

export const MARKET_DATA_IMAGE_TAG = '0.6.0'

export function evidenceKey(target: PluginLaunchTargetId, seat: PluginLaunchSeat): string {
  return `${target}:${seat}`
}

export function marketDataNamespace(_seat: PluginLaunchSeat): string {
  return 'plugin-market-data'
}

export function marketDataApplyCmd(_seat: PluginLaunchSeat): string {
  return 'cd bifrost-platform-plugin-market-data && kubectl apply -k k8s/base'
}

export function marketDataVerifyCmd(seat: PluginLaunchSeat): string {
  const ns = marketDataNamespace(seat)
  return [
    `kubectl -n ${ns} get deploy,cronjob`,
    `kubectl -n ${ns} get deploy -o jsonpath='{range .items[*]}{.metadata.name}={.spec.template.spec.containers[0].image}{"\\n"}{end}'`,
    seat === 'dev'
      ? 'make -C bifrost-platform-plugin-market-data verify-market-data'
      : `kubectl -n ${ns} exec deploy/market-data-api -- python -c "import urllib.request; print(urllib.request.urlopen('http://127.0.0.1:8790/health').read().decode())"`,
  ].join(' && ')
}

function emptyStore(): PluginLaunchStore {
  return {
    selectedTarget: 'ib-gateway',
    selectedSeat: 'dev',
    byKey: {},
  }
}

function migrateLegacyIfNeeded(store: PluginLaunchStore): PluginLaunchStore {
  try {
    const raw = localStorage.getItem(PLUGIN_LAUNCH_EVIDENCE_KEY)
    if (raw == null || raw === '') return store
    const legacy = JSON.parse(raw) as PluginLaunchEvidence
    const key = evidenceKey('ib-gateway', 'dev')
    if (store.byKey[key] == null && (legacy.lastInstallAt || legacy.lastVerifyAt || legacy.lastLiveCheckAt)) {
      store.byKey[key] = legacy
    }
  } catch {
    /* ignore */
  }
  return store
}

export function readPluginLaunchStore(): PluginLaunchStore {
  try {
    const raw = localStorage.getItem(PLUGIN_LAUNCH_STORE_KEY)
    if (raw != null && raw !== '') {
      const parsed = JSON.parse(raw) as PluginLaunchStore
      if (parsed.byKey != null && typeof parsed.byKey === 'object') {
        return migrateLegacyIfNeeded({
          selectedTarget: parsed.selectedTarget ?? 'ib-gateway',
          selectedSeat: parsed.selectedSeat ?? 'dev',
          byKey: parsed.byKey,
          updatedAt: parsed.updatedAt,
        })
      }
    }
  } catch {
    /* ignore */
  }
  return migrateLegacyIfNeeded(emptyStore())
}

export function writePluginLaunchStore(patch: Partial<PluginLaunchStore>): PluginLaunchStore {
  const cur = readPluginLaunchStore()
  const next: PluginLaunchStore = {
    ...cur,
    ...patch,
    byKey: patch.byKey ?? cur.byKey,
    updatedAt: new Date().toISOString(),
  }
  try {
    localStorage.setItem(PLUGIN_LAUNCH_STORE_KEY, JSON.stringify(next))
  } catch {
    /* ignore quota */
  }
  return next
}

/** Read evidence for one target/seat (defaults: current selection). */
export function readPluginLaunchEvidence(
  target?: PluginLaunchTargetId,
  seat?: PluginLaunchSeat,
): PluginLaunchEvidence {
  const store = readPluginLaunchStore()
  const t = target ?? store.selectedTarget
  const s = seat ?? (t === 'market-data' ? store.selectedSeat : 'dev')
  return store.byKey[evidenceKey(t, s)] ?? {}
}

export function writePluginLaunchEvidence(
  patch: Partial<PluginLaunchEvidence>,
  target?: PluginLaunchTargetId,
  seat?: PluginLaunchSeat,
): PluginLaunchEvidence {
  const store = readPluginLaunchStore()
  const t = target ?? store.selectedTarget
  const s = seat ?? (t === 'market-data' ? store.selectedSeat : 'dev')
  const key = evidenceKey(t, s)
  const nextEv: PluginLaunchEvidence = {
    ...(store.byKey[key] ?? {}),
    ...patch,
    updatedAt: new Date().toISOString(),
  }
  writePluginLaunchStore({
    byKey: { ...store.byKey, [key]: nextEv },
  })
  return nextEv
}

export function clearPluginLaunchEvidence(
  target?: PluginLaunchTargetId,
  seat?: PluginLaunchSeat,
): void {
  const store = readPluginLaunchStore()
  const t = target ?? store.selectedTarget
  const s = seat ?? (t === 'market-data' ? store.selectedSeat : 'dev')
  const key = evidenceKey(t, s)
  const byKey = { ...store.byKey }
  delete byKey[key]
  writePluginLaunchStore({ byKey })
}

/**
 * Clear this-cycle Detect → Live evidence after Published.
 * Keeps revisionHint; records lastPublished* for the strip.
 */
export function beginNextPluginLaunchCycle(
  target?: PluginLaunchTargetId,
  seat?: PluginLaunchSeat,
): PluginLaunchEvidence {
  const store = readPluginLaunchStore()
  const t = target ?? store.selectedTarget
  const s = seat ?? (t === 'market-data' ? store.selectedSeat : 'dev')
  const key = evidenceKey(t, s)
  const cur = store.byKey[key] ?? {}
  const nextEv: PluginLaunchEvidence = {
    revisionHint: cur.revisionHint,
    lastPublishedAt:
      cur.lastLiveCheckAt ?? cur.lastVerifyAt ?? cur.updatedAt ?? new Date().toISOString(),
    lastPublishedRevision: cur.revisionHint,
    updatedAt: new Date().toISOString(),
  }
  writePluginLaunchStore({
    byKey: { ...store.byKey, [key]: nextEv },
  })
  return nextEv
}

export function evidenceSummaryLine(ev: PluginLaunchEvidence): string {
  const bits: string[] = []
  if (ev.revisionHint) bits.push(`rev ${ev.revisionHint}`)
  if (ev.lastInstallAt) {
    bits.push(
      `Install ${ev.installOutcome ?? '?'} @ ${new Date(ev.lastInstallAt).toLocaleString()}`,
    )
  }
  if (ev.lastVerifyAt) {
    bits.push(
      `Verify ${ev.verifyOutcome ?? '?'} @ ${new Date(ev.lastVerifyAt).toLocaleString()}`,
    )
  }
  if (ev.lastLiveCheckAt) {
    bits.push(
      `Live ${ev.liveCheckOutcome ?? '?'} @ ${new Date(ev.lastLiveCheckAt).toLocaleString()}`,
    )
  }
  if (bits.length <= (ev.revisionHint ? 1 : 0) && ev.lastPublishedAt) {
    bits.push(`Last published @ ${new Date(ev.lastPublishedAt).toLocaleString()}`)
  }
  return bits.length > 0 ? bits.join(' · ') : 'No install/verify evidence yet'
}
