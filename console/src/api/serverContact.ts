import { useSyncExternalStore } from 'react'

let lastContact: number | null = null
const listeners = new Set<() => void>()

export function recordServerContact(): void {
  lastContact = Date.now()
  listeners.forEach(listener => listener())
}

export function getServerContact(): number | null { return lastContact }
export function subscribeServerContact(listener: () => void): () => void {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}
export function useServerContact(): number | null {
  return useSyncExternalStore(subscribeServerContact, getServerContact, getServerContact)
}

/** Records response headers, including errors; never records a failed/aborted fetch. */
export async function serverFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const response = await fetch(input, init)
  if (!init?.signal?.aborted) recordServerContact()
  return response
}

/** Test isolation without remounting the module's subscribers. */
export function resetServerContact(): void {
  lastContact = null
  listeners.forEach(listener => listener())
}
