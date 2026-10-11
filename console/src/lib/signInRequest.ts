import { useSyncExternalStore } from 'react'

let nonce = 0
const listeners = new Set<() => void>()

/** Ask the top-right sign-in to open. There is no second token field. */
export function requestSignIn(): void {
  nonce += 1
  for (const listener of listeners) listener()
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

export function useSignInRequest(): number {
  return useSyncExternalStore(
    subscribe,
    () => nonce,
    () => 0,
  )
}
