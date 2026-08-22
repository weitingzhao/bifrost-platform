import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  AGENT_LAUNCH_STORE_KEY,
  agentLaunchEvidenceKey,
  agentLaunchLastDeployDetail,
  isAgentLaunchLastDeployOk,
  readAgentLaunchEvidence,
  writeAgentLaunchEvidence,
  writeAgentLaunchStore,
} from '@/lib/delivery/agentLaunchEvidence'

function installMemoryLocalStorage(): void {
  const map = new Map<string, string>()
  vi.stubGlobal('localStorage', {
    getItem: (k: string) => map.get(k) ?? null,
    setItem: (k: string, v: string) => {
      map.set(k, String(v))
    },
    removeItem: (k: string) => {
      map.delete(k)
    },
    clear: () => {
      map.clear()
    },
    key: (i: number) => [...map.keys()][i] ?? null,
    get length() {
      return map.size
    },
  })
}

describe('agentLaunchEvidenceKey', () => {
  it('stores Both-target cycles under primary', () => {
    expect(agentLaunchEvidenceKey('both')).toBe('primary')
    expect(agentLaunchEvidenceKey('primary')).toBe('primary')
    expect(agentLaunchEvidenceKey('standby')).toBe('standby')
  })
})

describe('readAgentLaunchEvidence', () => {
  beforeEach(() => {
    installMemoryLocalStorage()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('reads primary evidence when the lane target is Both (sidebar vs page parity)', () => {
    writeAgentLaunchStore({ selectedTarget: 'both' })
    writeAgentLaunchEvidence({ deployOutcome: 'ok' }, 'primary')
    expect(readAgentLaunchEvidence().deployOutcome).toBe('ok')
    expect(readAgentLaunchEvidence('both').deployOutcome).toBe('ok')
    expect(localStorage.getItem(AGENT_LAUNCH_STORE_KEY)).not.toMatch(/"both":\{/)
  })
})

describe('isAgentLaunchLastDeployOk', () => {
  it('accepts API last=done, local evidence ok, or live runners', () => {
    expect(isAgentLaunchLastDeployOk('done', {})).toBe(true)
    expect(isAgentLaunchLastDeployOk(undefined, { deployOutcome: 'ok' })).toBe(true)
    expect(
      isAgentLaunchLastDeployOk(undefined, {}, { runnersLive: true, runnerVersion: '0.1.0' }),
    ).toBe(true)
    expect(isAgentLaunchLastDeployOk('failed', {})).toBe(false)
    expect(isAgentLaunchLastDeployOk(undefined, {})).toBe(false)
    expect(isAgentLaunchLastDeployOk(undefined, {}, { runnersLive: true })).toBe(false)
  })
})

describe('agentLaunchLastDeployDetail', () => {
  it('describes runner bootstrap when no API record', () => {
    expect(
      agentLaunchLastDeployDetail(undefined, {}, { runnersLive: true, runnerVersion: '0.1.0' }),
    ).toBe('runners live v0.1.0')
  })
})
