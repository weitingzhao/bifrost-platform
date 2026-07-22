import type { ActuationResponse } from './matrixTypes'
import type { StackAddonsResponse } from './deliveryTypes'
import { authedFetch } from './client'

export async function fetchStackAddons(): Promise<StackAddonsResponse> {
  const r = await fetch('/api/v1/stack/addons')
  if (!r.ok) throw new Error(`stack addons: HTTP ${r.status}`)
  return r.json() as Promise<StackAddonsResponse>
}

export async function installStackAddon(name: string): Promise<ActuationResponse> {
  const r = await authedFetch(
    'stack install',
    `/api/v1/stack/addons/${encodeURIComponent(name)}/install`,
    { method: 'POST' },
  )
  return r.json() as Promise<ActuationResponse>
}

export async function upgradeStackAddon(name: string): Promise<ActuationResponse> {
  const r = await authedFetch(
    'stack upgrade',
    `/api/v1/stack/addons/${encodeURIComponent(name)}/upgrade`,
    { method: 'POST' },
  )
  return r.json() as Promise<ActuationResponse>
}

